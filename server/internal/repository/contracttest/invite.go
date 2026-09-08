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

func testInviteRotateOnlyPending(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	creator := seedInviteCreator(t, repo)

	pending, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: "rotate-old", Role: "admin", CreatedBy: creator,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}

	rotated, err := repo.RotateInviteToken(ctx, pending.ID, "rotate-new")
	if err != nil {
		t.Fatalf("rotate pending: %v", err)
	}
	if rotated.ID != pending.ID || rotated.TokenHash != "rotate-new" || rotated.Role != "admin" || rotated.RedeemedAt != nil {
		t.Errorf("rotated row = %+v, want the same invite with the new hash", rotated)
	}
	if ok, err := repo.RedeemInvite(ctx, "rotate-old", "twitch-99"); err != nil || ok {
		t.Errorf("old token redeemed = %v, %v; want rejected", ok, err)
	}
	if ok, err := repo.RedeemInvite(ctx, "rotate-new", "twitch-99"); err != nil || !ok {
		t.Errorf("new token redeemed = %v, %v; want accepted", ok, err)
	}
	if _, err := repo.RotateInviteToken(ctx, pending.ID, "rotate-after-redeem"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("rotate redeemed = %v, want ErrNotFound", err)
	}

	expired, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: "rotate-expired", Role: "viewer", CreatedBy: creator,
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if _, err := repo.RotateInviteToken(ctx, expired.ID, "rotate-revived"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("rotate expired = %v, want ErrNotFound", err)
	}
	if _, err := repo.RotateInviteToken(ctx, expired.ID+1000, "rotate-unknown"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("rotate unknown = %v, want ErrNotFound", err)
	}
	if got, err := repo.GetInviteByTokenHash(ctx, "rotate-expired"); err != nil || got.TokenHash != "rotate-expired" {
		t.Errorf("expired row after failed rotate = %+v, %v; want untouched", got, err)
	}
}

// testInviteConcurrentRotateAndRedeem pins the race between an admin
// issuing a new link and the invitee redeeming the old one: exactly one
// side wins, and the old token is dead either way.
func testInviteConcurrentRotateAndRedeem(t *testing.T, h Harness) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := h.Repo()
	creator := seedInviteCreator(t, repo)
	inv, err := repo.CreateInvite(ctx, &repository.InviteInput{TokenHash: "race-old", Role: "viewer", CreatedBy: creator, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	var (
		redeemed  bool
		redeemErr error
		rotateErr error
	)
	start := make(chan struct{})
	done := make(chan struct{}, 2)
	go func() {
		<-start
		redeemed, redeemErr = repo.RedeemInvite(ctx, "race-old", "twitch-99")
		done <- struct{}{}
	}()
	go func() {
		<-start
		_, rotateErr = repo.RotateInviteToken(ctx, inv.ID, "race-new")
		done <- struct{}{}
	}()
	close(start)
	<-done
	<-done
	if redeemErr != nil {
		t.Fatalf("redeem: %v", redeemErr)
	}
	if rotateErr != nil && !errors.Is(rotateErr, repository.ErrNotFound) {
		t.Fatalf("rotate: %v", rotateErr)
	}
	rotated := rotateErr == nil
	if redeemed == rotated {
		t.Fatalf("redeemed=%v rotated=%v; exactly one side must win", redeemed, rotated)
	}
	hash := "race-old"
	if rotated {
		hash = "race-new"
	}
	stored, err := repo.GetInviteByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("get %s: %v", hash, err)
	}
	if redeemed && (stored.RedeemedAt == nil || stored.RedeemedBy == nil || *stored.RedeemedBy != "twitch-99") {
		t.Errorf("redeem won but row is not marked redeemed: %+v", stored)
	}
	if rotated && stored.RedeemedAt != nil {
		t.Errorf("rotate won but row is marked redeemed: %+v", stored)
	}
	if ok, err := repo.RedeemInvite(ctx, "race-old", "twitch-100"); err != nil || ok {
		t.Errorf("old token redeemed after the race = %v, %v; want rejected", ok, err)
	}
}
