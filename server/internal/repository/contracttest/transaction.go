package contracttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testTransactionCommitAndRollback(t *testing.T, h Harness) {
	repo := h.Repo()
	creator := seedInviteCreator(t, repo)
	stop := errors.New("abort transaction")
	for _, outcome := range []string{"commit", "error", "cancel", "panic"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", outcome, existing), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				id := fmt.Sprintf("%s-%t", outcome, existing)
				user := &repository.User{ID: id, Login: id, DisplayName: "Before", Role: "viewer"}
				if existing {
					if _, err := repo.UpsertUser(ctx, user); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := repo.CreateInvite(ctx, &repository.InviteInput{
					TokenHash: id, Role: "admin", CreatedBy: creator, ExpiresAt: time.Now().Add(time.Hour),
				}); err != nil {
					t.Fatal(err)
				}
				var txErr error
				panicked := func() (caught any) {
					defer func() { caught = recover() }()
					txErr = repo.WithTx(ctx, func(tx repository.Repository) error {
						if ok, err := tx.RedeemInvite(ctx, id, id); err != nil || !ok {
							return fmt.Errorf("claim invite: ok=%v, err=%v", ok, err)
						}
						user.DisplayName = "After"
						if _, err := tx.UpsertUser(ctx, user); err != nil {
							return err
						}
						if err := tx.UpdateUserRole(ctx, id, "admin"); err != nil {
							return err
						}
						if err := tx.AddToWhitelist(ctx, id); err != nil {
							return err
						}
						switch outcome {
						case "error":
							return stop
						case "cancel":
							cancel() // All writes succeeded, but commit must fail.
						case "panic":
							panic(stop)
						}
						return nil
					})
					return nil
				}()
				switch outcome {
				case "commit":
					if txErr != nil || panicked != nil {
						t.Fatalf("commit: err=%v, panic=%v", txErr, panicked)
					}
				case "error":
					if !errors.Is(txErr, stop) || panicked != nil {
						t.Fatalf("callback error was lost: err=%v, panic=%v", txErr, panicked)
					}
				case "cancel":
					if txErr == nil || panicked != nil {
						t.Fatalf("cancelled transaction: err=%v, panic=%v", txErr, panicked)
					}
				case "panic":
					if panicked != stop {
						t.Fatalf("panic = %v, want %v", panicked, stop)
					}
				}
				readCtx := context.Background()
				inv, err := repo.GetInviteByTokenHash(readCtx, id)
				if err != nil {
					t.Fatal(err)
				}
				committed := outcome == "commit"
				if (inv.RedeemedAt != nil) != committed || (inv.RedeemedBy != nil) != committed {
					t.Errorf("invite escaped transaction boundary: %+v", inv)
				}
				if allowed, err := repo.IsWhitelisted(readCtx, id); err != nil || allowed != committed {
					t.Errorf("whitelist escaped transaction boundary: %v, %v", allowed, err)
				}
				stored, err := repo.GetUser(readCtx, id)
				if !existing && !committed {
					if !errors.Is(err, repository.ErrNotFound) {
						t.Errorf("rolled back insert left a user: %+v, %v", stored, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				wantRole, wantName := "viewer", "Before"
				if committed {
					wantRole, wantName = "admin", "After"
				}
				if stored.Role != wantRole || stored.DisplayName != wantName {
					t.Errorf("profile or role escaped transaction boundary: %+v", stored)
				}
			})
		}
	}
}
