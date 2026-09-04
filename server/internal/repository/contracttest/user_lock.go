package contracttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testUserLockSerializesRoleChanges(t *testing.T, h Harness) {
	repo := h.Repo()
	writerRepo := h.ConcurrentRepo(t)
	for _, existing := range []bool{true, false} {
		for _, rollback := range []bool{false, true} {
			t.Run(fmt.Sprintf("existing=%t/rollback=%t", existing, rollback), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				id := fmt.Sprintf("user-%t-%t", existing, rollback)
				user := &repository.User{ID: id, Login: id, DisplayName: id, Role: "viewer"}
				if existing {
					if _, err := repo.UpsertUser(ctx, user); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := repo.GetUserForUpdate(ctx, id); err == nil {
					t.Fatal("locking outside a transaction must fail")
				}
				locked := make(chan struct{})
				release := make(chan struct{})
				finished := make(chan error, 1)
				abort := errors.New("roll back")
				go func() {
					finished <- repo.WithTx(ctx, func(tx repository.Repository) error {
						got, err := tx.GetUserForUpdate(ctx, id)
						if existing {
							if err != nil || got.Role != "viewer" {
								return fmt.Errorf("locked user: %+v, %v", got, err)
							}
						} else if !errors.Is(err, repository.ErrNotFound) {
							return fmt.Errorf("missing lock: %v", err)
						}
						close(locked)
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
						if rollback {
							return abort
						}
						return nil
					})
				}()
				select {
				case <-locked:
				case err := <-finished:
					t.Fatalf("lock failed: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				writer := make(chan error, 1)
				go func() {
					if existing {
						writer <- writerRepo.UpdateUserRole(ctx, id, "owner")
					} else {
						user.Role = "owner"
						_, err := writerRepo.UpsertUser(ctx, user)
						writer <- err
					}
				}()
				select {
				case err := <-writer:
					close(release)
					<-finished
					t.Fatalf("role/signup write escaped user lock: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				close(release)
				err := <-finished
				if rollback {
					if !errors.Is(err, abort) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if err := <-writer; err != nil {
					t.Fatalf("writer after release: %v", err)
				}
				got, err := repo.GetUser(ctx, id)
				if err != nil || got.Role != "owner" {
					t.Fatalf("promotion lost: %+v, %v", got, err)
				}
			})
		}
	}
}
