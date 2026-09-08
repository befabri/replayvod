package playbackauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type validatorFunc func(context.Context, string) (Identity, error)

func (f validatorFunc) Validate(ctx context.Context, token string) (Identity, error) {
	return f(ctx, token)
}

func TestConnectionLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	secret := strings.Repeat("s", 32)
	var validationErr error
	calls := 0
	validator := validatorFunc(func(_ context.Context, token string) (Identity, error) {
		calls++
		if token != testToken {
			t.Error("wrong token passed to validator")
		}
		return Identity{UserID: "123", Login: "viewer"}, validationErr
	})
	s := New(repo, secret, validator)
	if token, err := s.Token(ctx); token != "" || err != nil {
		t.Fatalf("anonymous: %v", err)
	}
	connected, err := s.Connect(ctx, testToken)
	if err != nil || connected.State != "connected" {
		t.Fatalf("connect: %+v %v", connected, err)
	}
	row, err := repo.GetTwitchPlaybackSession(ctx)
	if err != nil || len(row.EncryptedToken) == 0 || bytes.Contains(row.EncryptedToken, []byte(testToken)) {
		t.Fatal("credential was not encrypted")
	}
	encoded, _ := json.Marshal(connected)
	if bytes.Contains(encoded, []byte(testToken)) || bytes.Contains(encoded, []byte("token")) {
		t.Fatal("credential exposed in status")
	}
	// A fresh service models restart; it must use the persisted session without
	// requiring a dashboard login or another validation within the hour.
	s = New(repo, secret, validator)
	if token, err := s.Token(ctx); token != testToken || err != nil || calls != 1 {
		t.Fatalf("restart: %v calls=%d", err, calls)
	}
	validationErr = ErrRejected
	if _, err := s.Connect(ctx, testToken); !errors.Is(err, ErrRejected) {
		t.Fatal("invalid replacement accepted")
	}
	unchanged, _ := repo.GetTwitchPlaybackSession(ctx)
	if !bytes.Equal(row.EncryptedToken, unchanged.EncryptedToken) {
		t.Fatal("failed replacement overwrote connection")
	}
	validationErr = ErrUnavailable
	if _, err := s.Check(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("transient validation = %v", err)
	}
	got, _ := s.Status(ctx)
	if got.State != "connected" {
		t.Fatal("outage invalidated session")
	}
	validationErr = ErrRejected
	got, err = s.Check(ctx)
	if err != nil || got.State != "reconnect_required" {
		t.Fatalf("revoked status = %+v %v", got, err)
	}
	if _, err := s.Token(ctx); !errors.Is(err, ErrRejected) {
		t.Fatal("revoked session silently fell back")
	}
	validationErr = nil
	if _, err := s.Connect(ctx, testToken); err != nil {
		t.Fatal(err)
	}
	got, err = New(repo, strings.Repeat("x", 32), validator).Status(ctx)
	if err != nil || got.State != "reconnect_required" {
		t.Fatal("changed encryption key not surfaced")
	}
	got, err = s.Disconnect(ctx)
	if err != nil || got.State != "disconnected" {
		t.Fatal("disconnect failed")
	}
	if _, err := repo.GetTwitchPlaybackSession(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("disconnect did not remove saved credential")
	}
	if token, err := s.Token(ctx); token != "" || err != nil {
		t.Fatal("disconnect did not restore anonymous playback")
	}
}

func TestTokenRevalidatesStaleConnection(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	var rejected bool
	validator := validatorFunc(func(context.Context, string) (Identity, error) {
		if rejected {
			return Identity{}, ErrRejected
		}
		return Identity{UserID: "123", Login: "viewer"}, nil
	})
	s := New(repo, strings.Repeat("s", 32), validator)
	if _, err := s.Connect(ctx, testToken); err != nil {
		t.Fatal(err)
	}
	row, _ := repo.GetTwitchPlaybackSession(ctx)
	row.CheckedAt = time.Now().Add(-2 * time.Hour).Unix()
	if err := repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
		t.Fatal(err)
	}
	rejected = true
	if _, err := s.Token(ctx); !errors.Is(err, ErrRejected) {
		t.Fatalf("stale token = %v", err)
	}
	got, _ := s.Status(ctx)
	if got.State != "reconnect_required" {
		t.Fatal("revalidation was not persisted")
	}
}

func TestStaleRejectionCannotInvalidateReplacement(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	s := New(repo, strings.Repeat("s", 32), validatorFunc(func(context.Context, string) (Identity, error) { return Identity{UserID: "123", Login: "viewer"}, nil }))
	if _, err := s.Connect(ctx, testToken); err != nil {
		t.Fatal(err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	s.validator = validatorFunc(func(context.Context, string) (Identity, error) {
		close(started)
		<-finish
		return Identity{}, ErrRejected
	})
	done := make(chan error, 1)
	go func() { done <- s.RecheckRejected(ctx, testToken) }()
	<-started
	replacement := New(repo, strings.Repeat("s", 32), validatorFunc(func(context.Context, string) (Identity, error) {
		return Identity{UserID: "456", Login: "replacement"}, nil
	}))
	if _, err := replacement.Connect(ctx, "replacement-0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	close(finish)
	if err := <-done; !errors.Is(err, ErrChanged) {
		t.Fatalf("stale rejection = %v", err)
	}
	got, _ := s.Status(ctx)
	if got.State != "connected" || got.Login != "replacement" {
		t.Fatalf("replacement invalidated: %+v", got)
	}
}

func TestTokenReloadsConnectionAfterInFlightValidation(t *testing.T) {
	for _, outcome := range []struct {
		name string
		err  error
	}{{"valid", nil}, {"revoked", ErrRejected}, {"outage", ErrUnavailable}} {
		for _, disconnect := range []bool{false, true} {
			name := outcome.name + "/replace"
			if disconnect {
				name = outcome.name + "/disconnect"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
				secret := strings.Repeat("s", 32)
				s := New(repo, secret, validatorFunc(func(context.Context, string) (Identity, error) { return Identity{UserID: "123", Login: "viewer"}, nil }))
				if _, err := s.Connect(ctx, testToken); err != nil {
					t.Fatal(err)
				}
				row, _ := repo.GetTwitchPlaybackSession(ctx)
				row.CheckedAt = time.Now().Add(-2 * time.Hour).Unix()
				if err := repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
					t.Fatal(err)
				}
				started, finish := make(chan struct{}), make(chan struct{})
				s.validator = validatorFunc(func(context.Context, string) (Identity, error) {
					close(started)
					<-finish
					return Identity{UserID: "123", Login: "viewer"}, outcome.err
				})
				type result struct {
					token string
					err   error
				}
				done := make(chan result, 1)
				go func() { token, err := s.Token(ctx); done <- result{token, err} }()
				<-started
				replacement := New(repo, secret, validatorFunc(func(context.Context, string) (Identity, error) {
					return Identity{UserID: "456", Login: "replacement"}, nil
				}))
				want := "replacement-session-0123456789abcdef"
				if disconnect {
					want = ""
					if _, err := replacement.Disconnect(ctx); err != nil {
						t.Fatal(err)
					}
				} else if _, err := replacement.Connect(ctx, want); err != nil {
					t.Fatal(err)
				}
				close(finish)
				got := <-done
				if got.err != nil || got.token != want {
					t.Fatalf("in-flight request did not reload current connection: err=%v", got.err)
				}
			})
		}
	}
}

func TestConcurrentTokenRequestsValidateOnceAndWaitersCanCancel(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	var calls atomic.Int32
	started, finish := make(chan struct{}), make(chan struct{})
	s := New(repo, strings.Repeat("s", 32), validatorFunc(func(context.Context, string) (Identity, error) {
		if calls.Add(1) > 1 {
			close(started)
			<-finish
		}
		return Identity{UserID: "123", Login: "viewer"}, nil
	}))
	if _, err := s.Connect(ctx, testToken); err != nil {
		t.Fatal(err)
	}
	row, _ := repo.GetTwitchPlaybackSession(ctx)
	row.CheckedAt = time.Now().Add(-2 * time.Hour).Unix()
	if err := repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			token, err := s.Token(ctx)
			if err != nil || token != testToken {
				t.Errorf("token request: %v", err)
			}
		})
	}
	<-started
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Token(canceled); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled waiter: %v", err)
	}
	close(finish)
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatalf("validation calls=%d, want connect + one shared validation", calls.Load())
	}
}

func TestExpiredSessionIsNotReusedOrRevalidated(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	calls := 0
	s := New(repo, strings.Repeat("s", 32), validatorFunc(func(context.Context, string) (Identity, error) {
		calls++
		return Identity{UserID: "123", Login: "viewer", ExpiresAt: time.Now().Add(-time.Second).Unix()}, nil
	}))
	if _, err := s.Connect(ctx, testToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Token(ctx); !errors.Is(err, ErrRejected) {
		t.Fatalf("expired token: %v", err)
	}
	if calls != 1 {
		t.Fatal("known expiry was revalidated")
	}
}

type failingStore struct {
	Store
	loadErr, updateErr error
}

func (r *failingStore) GetTwitchPlaybackSession(ctx context.Context) (*repository.TwitchPlaybackSession, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return r.Store.GetTwitchPlaybackSession(ctx)
}
func (r *failingStore) UpdateTwitchPlaybackSessionValidation(ctx context.Context, row *repository.TwitchPlaybackSession) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.Store.UpdateTwitchPlaybackSessionValidation(ctx, row)
}

func TestTokenStoreFailuresAreRetryableWithoutInvalidatingTheSession(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	store := &failingStore{Store: repo}
	s := New(store, strings.Repeat("s", 32), validatorFunc(func(context.Context, string) (Identity, error) { return Identity{UserID: "123", Login: "viewer"}, nil }))
	ctx := t.Context()
	if _, err := s.Connect(ctx, testToken); err != nil {
		t.Fatal(err)
	}
	privateError := errors.New("database unavailable: private diagnostic")
	store.loadErr = privateError
	if _, err := s.Token(ctx); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private diagnostic") {
		t.Fatalf("read failure = %v", err)
	}
	store.loadErr = nil
	store.updateErr = privateError
	if _, err := s.Check(ctx); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private diagnostic") {
		t.Fatalf("update failure = %v", err)
	}
	store.updateErr = nil
	token, err := s.Token(ctx)
	if err != nil || token != testToken {
		t.Fatalf("session did not recover: err=%v", err)
	}
	row, err := repo.GetTwitchPlaybackSession(ctx)
	if err != nil || row.NeedsReconnect {
		t.Fatalf("outage invalidated credential: %+v, %v", row, err)
	}
}
