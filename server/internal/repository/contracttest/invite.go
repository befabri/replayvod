package contracttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func seedInviteCreator(t *testing.T, repo repository.Repository) string {
	t.Helper()
	u, err := repo.UpsertUser(context.Background(), &repository.User{
		ID: "u-inviter", Login: "inviter", DisplayName: "Inviter", Role: "admin",
	})
	if err != nil {
		t.Fatalf("seed creator: %v", err)
	}
	return u.ID
}

func testInviteRedeemSingleUse(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	creator := seedInviteCreator(t, repo)

	created, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: "hash-1", Role: "viewer", CreatedBy: creator,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ok, err := repo.RedeemInvite(ctx, "hash-1", "twitch-99")
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if !ok {
		t.Fatal("first redemption = false, want true")
	}

	got, err := repo.GetInviteByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("get after redeem: %v", err)
	}
	if got.ID != created.ID || got.RedeemedAt == nil || got.RedeemedBy == nil || *got.RedeemedBy != "twitch-99" {
		t.Errorf("redeemed row not marked: %+v", got)
	}

	ok, err = repo.RedeemInvite(ctx, "hash-1", "twitch-100")
	if err != nil {
		t.Fatalf("second redeem: %v", err)
	}
	if ok {
		t.Error("second redemption = true, want false (single-use)")
	}
}

func testInviteRedeemExpiredFailsClosed(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	creator := seedInviteCreator(t, repo)

	if _, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: "hash-expired", Role: "admin", CreatedBy: creator,
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ok, err := repo.RedeemInvite(ctx, "hash-expired", "twitch-99")
	if err != nil {
		t.Fatalf("redeem expired: %v", err)
	}
	if ok {
		t.Error("expired redemption = true, want false")
	}

	ok, err = repo.RedeemInvite(ctx, "hash-unknown", "twitch-99")
	if err != nil {
		t.Fatalf("redeem unknown: %v", err)
	}
	if ok {
		t.Error("unknown-token redemption = true, want false")
	}
}

func testInviteRevokeOnlyPending(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	creator := seedInviteCreator(t, repo)

	note := "for alice"
	pending, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: "hash-pending", Role: "viewer", Note: &note, CreatedBy: creator,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	redeemed, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: "hash-redeemed", Role: "viewer", CreatedBy: creator,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create redeemed: %v", err)
	}
	if ok, err := repo.RedeemInvite(ctx, "hash-redeemed", "twitch-99"); err != nil || !ok {
		t.Fatalf("redeem: ok=%v err=%v", ok, err)
	}

	if ok, err := repo.DeleteInvite(ctx, pending.ID); err != nil || !ok {
		t.Fatalf("delete pending: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.DeleteInvite(ctx, redeemed.ID); err != nil {
		t.Fatalf("delete redeemed: %v", err)
	} else if ok {
		t.Error("delete redeemed = true, want false (kept for audit)")
	}

	rows, err := repo.ListInvites(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != redeemed.ID {
		t.Fatalf("list after revoke = %+v, want only the redeemed row", rows)
	}
	if rows[0].Note != nil {
		t.Errorf("redeemed row note = %v, want nil", *rows[0].Note)
	}
}

func testInviteConcurrentRedemption(t *testing.T, h Harness) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := h.Repo()
	creator := seedInviteCreator(t, repo)
	if _, err := repo.CreateInvite(ctx, &repository.InviteInput{TokenHash: "shared-token", Role: "admin", CreatedBy: creator, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	type result struct {
		user string
		ok   bool
		err  error
	}
	const contenders = 8
	start := make(chan struct{})
	results := make(chan result, contenders)
	for i := range contenders {
		go func() {
			<-start
			user := fmt.Sprintf("redeemer-%d", i)
			var ok bool
			err := repo.WithTx(ctx, func(tx repository.Repository) error {
				var err error
				ok, err = tx.RedeemInvite(ctx, "shared-token", user)
				if err != nil || !ok {
					return err
				}
				if _, err := tx.UpsertUser(ctx, &repository.User{ID: user, Login: user, DisplayName: user, Role: "admin"}); err != nil {
					return err
				}
				return tx.AddToWhitelist(ctx, user)
			})
			results <- result{user, ok && err == nil, err}
		}()
	}
	close(start)
	winner := ""
	for range contenders {
		r := <-results
		if r.err != nil {
			t.Errorf("redeem as %s: %v", r.user, r.err)
		}
		if r.ok {
			if winner != "" {
				t.Errorf("both %s and %s redeemed one invite", winner, r.user)
			}
			winner = r.user
		}
		if allowed, err := repo.IsWhitelisted(ctx, r.user); err != nil || allowed != r.ok {
			t.Errorf("whitelist for %s = %v, %v; want only the winner admitted", r.user, allowed, err)
		}
		user, err := repo.GetUser(ctx, r.user)
		if r.ok {
			if err != nil || user.Role != "admin" {
				t.Errorf("winning account = %+v, %v", user, err)
			}
		} else if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("losing transaction left an account: %+v, %v", user, err)
		}
	}
	stored, err := repo.GetInviteByTokenHash(ctx, "shared-token")
	if err != nil {
		t.Fatal(err)
	}
	if winner == "" || stored.RedeemedAt == nil || stored.RedeemedBy == nil || *stored.RedeemedBy != winner {
		t.Fatalf("winner=%q, invite=%+v; want one winner with matching audit data", winner, stored)
	}
}
