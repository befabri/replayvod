package integration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/system"
)

// failingManagementRepo injects late failures inside real transactions to
// expose incomplete rollback.
type failingManagementRepo struct {
	repository.Repository
	stage string
	err   error
}

func (r failingManagementRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		r.Repository = tx
		if err := fn(r); err != nil {
			return err
		}
		if r.stage == "before commit" {
			return r.err
		}
		return nil
	})
}

func (r failingManagementRepo) GetUserForUpdate(ctx context.Context, id string) (*repository.User, error) {
	if r.stage == "permission read" {
		return nil, r.err
	}
	return r.Repository.GetUserForUpdate(ctx, id)
}

func (r failingManagementRepo) GetUser(ctx context.Context, id string) (*repository.User, error) {
	if r.stage == "response read" {
		return nil, r.err
	}
	return r.Repository.GetUser(ctx, id)
}

func (r failingManagementRepo) RemoveFromWhitelist(ctx context.Context, id string) error {
	if err := r.Repository.RemoveFromWhitelist(ctx, id); err != nil {
		return err
	}
	if r.stage == "whitelist removal" {
		return r.err
	}
	return nil
}

func (r failingManagementRepo) DeleteUserSessions(ctx context.Context, id string) error {
	if err := r.Repository.DeleteUserSessions(ctx, id); err != nil {
		return err
	}
	if r.stage == "session revocation" {
		return r.err
	}
	return nil
}

func TestUserManagementFailuresRollBackAndAllowRetry(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, tc := range []struct{ operation, stage string }{
			{"role", "permission read"},
			{"role", "response read"},
			{"role", "before commit"},
			{"whitelist", "permission read"},
			{"whitelist", "whitelist removal"},
			{"whitelist", "session revocation"},
			{"whitelist", "before commit"},
		} {
			t.Run(backend+"/"+tc.operation+"/"+tc.stage, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				repo, _ := repositories(t, backend)
				seedAccess(t, repo)
				stop := errors.New("injected database failure")
				log := slog.New(slog.NewTextHandler(io.Discard, nil))
				admin := &repository.User{ID: "admin", Role: "admin"}
				write := func(target repository.Repository) error {
					svc := system.New(target, log)
					if tc.operation == "whitelist" {
						return svc.RemoveFromWhitelist(ctx, admin, "target")
					}
					user, err := svc.UpdateUserRole(ctx, admin, "target", "admin")
					if err != nil && user != nil {
						t.Error("failed transaction returned an uncommitted user")
					}
					return err
				}
				if err := write(failingManagementRepo{Repository: repo, stage: tc.stage, err: stop}); !errors.Is(err, stop) {
					t.Fatalf("write error = %v, want injected failure", err)
				}
				user, err := repo.GetUser(ctx, "target")
				if err != nil || user.Role != "viewer" {
					t.Errorf("failed write changed role: %+v, %v", user, err)
				}
				if allowed, err := repo.IsWhitelisted(ctx, "target"); err != nil || !allowed {
					t.Errorf("failed write removed whitelist access: %v, %v", allowed, err)
				}
				if sessions, err := repo.ListUserSessions(ctx, "target"); err != nil || len(sessions) != 1 {
					t.Errorf("failed write revoked sessions: %+v, %v", sessions, err)
				}
				if err := write(repo); err != nil {
					t.Fatalf("retry: %v", err)
				}
				if tc.operation == "role" {
					user, err := repo.GetUser(ctx, "target")
					if err != nil || user.Role != "admin" {
						t.Errorf("retry did not save role: %+v, %v", user, err)
					}
				} else {
					if allowed, err := repo.IsWhitelisted(ctx, "target"); err != nil || allowed {
						t.Errorf("retry did not remove whitelist access: %v, %v", allowed, err)
					}
					if sessions, err := repo.ListUserSessions(ctx, "target"); err != nil || len(sessions) != 0 {
						t.Errorf("retry did not revoke sessions: %+v, %v", sessions, err)
					}
					if err := write(repo); err != nil {
						t.Errorf("repeated removal must stay idempotent: %v", err)
					}
				}
			})
		}
	}
}
