package integration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/system"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.SetupPG(m)) }

// repositories uses independent connections so Go pool limits cannot hide
// missing database locks.
func repositories(t *testing.T, backend string) (repository.Repository, repository.Repository) {
	t.Helper()
	if backend == "postgres" {
		pool := testdb.NewPGPool(t)
		return pgadapter.New(pool), pgadapter.New(pool)
	}
	db := testdb.NewSQLiteDB(t)
	var seq int
	var name, file string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &file); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatal(err)
	}
	other, err := database.NewSQLiteDB(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	if _, err := other.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatal(err)
	}
	return sqliteadapter.New(db), sqliteadapter.New(other)
}

func seedAccess(t *testing.T, repo repository.Repository) *repository.User {
	t.Helper()
	ctx := t.Context()
	user, err := repo.UpsertUser(ctx, &repository.User{ID: "target", Login: "target", DisplayName: "Target", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AddToWhitelist(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateSession(ctx, &repository.Session{
		HashedID: "target-session", UserID: user.ID, EncryptedTokens: []byte("tokens"), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return user
}

// observedReadRepo coordinates interleavings around real permission reads and
// writes.
type observedReadRepo struct {
	repository.Repository
	beforeLock func()
	afterRead  func()
}

func (r observedReadRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		r.Repository = tx
		return fn(r)
	})
}

func (r observedReadRepo) GetUser(ctx context.Context, id string) (*repository.User, error) {
	u, err := r.Repository.GetUser(ctx, id)
	if r.beforeLock != nil {
		r.beforeLock() // The unlocked read has already captured the role.
	}
	if r.afterRead != nil {
		r.afterRead()
	}
	return u, err
}

func (r observedReadRepo) GetUserForUpdate(ctx context.Context, id string) (*repository.User, error) {
	if r.beforeLock != nil {
		r.beforeLock()
	}
	u, err := r.Repository.GetUserForUpdate(ctx, id)
	if r.afterRead != nil {
		r.afterRead()
	}
	return u, err
}

func adminWrite(ctx context.Context, repo repository.Repository, operation string) error {
	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	admin := &repository.User{ID: "admin", Role: "admin"}
	if operation == "role" {
		_, err := svc.UpdateUserRole(ctx, admin, "target", "viewer")
		return err
	}
	return svc.RemoveFromWhitelist(ctx, admin, "target")
}

func waitSignal(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func waitResult(t *testing.T, ctx context.Context, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return ctx.Err()
	}
}

var permissionScenarios = []struct {
	name, operation string
	missing         bool
}{
	{"role", "role", false},
	{"whitelist", "whitelist", false},
	{"whitelist before signup", "whitelist", true},
}

func seedScenario(t *testing.T, repo repository.Repository, missing bool) {
	t.Helper()
	if missing {
		if err := repo.AddToWhitelist(t.Context(), "target"); err != nil {
			t.Fatal(err)
		}
		return
	}
	seedAccess(t, repo)
}

func promoteOwner(ctx context.Context, repo repository.Repository, missing bool) error {
	if missing {
		_, err := repo.UpsertUser(ctx, &repository.User{ID: "target", Login: "target", DisplayName: "Target", Role: "owner"})
		return err
	}
	return repo.UpdateUserRole(ctx, "target", "owner")
}

func TestOwnerPromotionBeforePermissionCheck(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, tc := range permissionScenarios {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				repo, other := repositories(t, backend)
				seedScenario(t, repo, tc.missing)
				promoted, release := make(chan struct{}), make(chan struct{})
				unlock := sync.OnceFunc(func() { close(release) })
				defer unlock()
				promotion := make(chan error, 1)
				go func() {
					promotion <- other.WithTx(ctx, func(tx repository.Repository) error {
						if err := promoteOwner(ctx, tx, tc.missing); err != nil {
							return err
						}
						close(promoted)
						select {
						case <-release:
							return nil
						case <-ctx.Done():
							return ctx.Err()
						}
					})
				}()
				waitSignal(t, ctx, promoted)
				reading := make(chan struct{})
				observed := observedReadRepo{Repository: repo, beforeLock: sync.OnceFunc(func() { close(reading) })}
				write := make(chan error, 1)
				go func() { write <- adminWrite(ctx, observed, tc.operation) }()
				waitSignal(t, ctx, reading)
				unlock()
				if err := waitResult(t, ctx, promotion); err != nil {
					t.Fatal(err)
				}
				if err := waitResult(t, ctx, write); !errors.Is(err, system.ErrOwnerRoleRequired) {
					t.Errorf("admin write = %v, want ErrOwnerRoleRequired", err)
				}
				user, err := repo.GetUser(ctx, "target")
				if err != nil || user.Role != "owner" {
					t.Errorf("promoted owner was changed: %+v, %v", user, err)
				}
				if allowed, err := repo.IsWhitelisted(ctx, "target"); err != nil || !allowed {
					t.Errorf("promoted owner lost whitelist access: %v, %v", allowed, err)
				}
				wantSessions := 1
				if tc.missing {
					wantSessions = 0
				}
				if sessions, err := repo.ListUserSessions(ctx, "target"); err != nil || len(sessions) != wantSessions {
					t.Errorf("promoted owner lost sessions: %+v, %v", sessions, err)
				}
			})
		}
	}
}

func TestOwnerPromotionWaitsForPermissionWrite(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, tc := range permissionScenarios {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				repo, other := repositories(t, backend)
				seedScenario(t, repo, tc.missing)
				checked, release := make(chan struct{}), make(chan struct{})
				unlock := sync.OnceFunc(func() { close(release) })
				defer unlock()
				observed := observedReadRepo{Repository: repo, afterRead: sync.OnceFunc(func() {
					close(checked)
					select {
					case <-release:
					case <-ctx.Done():
					}
				})}
				write := make(chan error, 1)
				go func() { write <- adminWrite(ctx, observed, tc.operation) }()
				waitSignal(t, ctx, checked)
				promotion := make(chan error, 1)
				go func() { promotion <- promoteOwner(ctx, other, tc.missing) }()
				select {
				case err := <-promotion:
					unlock()
					_ = waitResult(t, ctx, write)
					t.Fatalf("promotion finished between permission check and write: %v", err)
				case <-time.After(100 * time.Millisecond):
					// The promotion must wait until the protected write commits.
				}
				unlock()
				if err := waitResult(t, ctx, write); err != nil {
					t.Fatal(err)
				}
				if err := waitResult(t, ctx, promotion); err != nil {
					t.Fatal(err)
				}
				user, err := repo.GetUser(ctx, "target")
				if err != nil || user.Role != "owner" {
					t.Errorf("later promotion was overwritten: %+v, %v", user, err)
				}
			})
		}
	}
}

func TestUserLockRequiresTransactionAndPreservesProfile(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			repo, other := repositories(t, backend)
			before := seedAccess(t, repo)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if _, err := repo.GetUserForUpdate(ctx, "target"); err == nil {
				t.Fatal("locking outside a transaction must fail")
			}
			if err := repo.WithTx(ctx, func(tx repository.Repository) error {
				_, err := tx.GetUserForUpdate(ctx, "missing")
				return err
			}); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("missing user = %v, want ErrNotFound", err)
			}
			stop := errors.New("abort permission write")
			for _, outcome := range []error{nil, stop} {
				err := repo.WithTx(ctx, func(tx repository.Repository) error {
					if _, err := tx.GetUserForUpdate(ctx, "target"); err != nil {
						return err
					}
					return outcome
				})
				if !errors.Is(err, outcome) {
					t.Fatalf("transaction = %v, want %v", err, outcome)
				}
				after, err := other.GetUser(ctx, "target")
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("lock changed profile or timestamps: before=%+v, after=%+v, err=%v", before, after, err)
				}
			}
			if err := other.UpdateUserRole(ctx, "target", "owner"); err != nil {
				t.Fatalf("lock remained held after rollback: %v", err)
			}
		})
	}
}
