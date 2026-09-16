package contracttest

import (
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testSessionLifecycle(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "session-channel")
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "guest", Login: "guest", DisplayName: "guest", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	agent, ip := "agent/1.0", "203.0.113.7"
	for _, s := range []*repository.Session{
		{HashedID: "live", UserID: "owner", EncryptedTokens: []byte("t1"), ExpiresAt: now.Add(time.Hour), UserAgent: &agent, IPAddress: &ip},
		{HashedID: "expired", UserID: "owner", EncryptedTokens: []byte("t2"), ExpiresAt: now.Add(-time.Hour)},
		{HashedID: "guest", UserID: "guest", EncryptedTokens: []byte("t3"), ExpiresAt: now.Add(time.Hour)},
	} {
		if err := repo.CreateSession(ctx, s); err != nil {
			t.Fatalf("create session %s: %v", s.HashedID, err)
		}
	}
	got, err := repo.GetSession(ctx, "live")
	if err != nil || got.HashedID != "live" || got.UserID != "owner" || string(got.EncryptedTokens) != "t1" || !got.ExpiresAt.Equal(now.Add(time.Hour)) ||
		got.UserAgent == nil || *got.UserAgent != agent || got.IPAddress == nil || *got.IPAddress != ip || got.LastActiveAt.IsZero() || got.CreatedAt.IsZero() {
		t.Fatalf("session = %+v, %v", got, err)
	}
	if _, err := repo.GetSession(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing session: %v", err)
	}
	if err := repo.UpdateSessionTokens(ctx, "live", []byte("t1-rotated")); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateSessionActivity(ctx, "live"); err != nil {
		t.Fatal(err)
	}
	rotated, err := repo.GetSession(ctx, "live")
	if err != nil || string(rotated.EncryptedTokens) != "t1-rotated" || rotated.LastActiveAt.Before(got.LastActiveAt) {
		t.Fatalf("rotated session = %+v, %v", rotated, err)
	}
	if err := repo.DeleteExpiredSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSession(ctx, "expired"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expired session survived: %v", err)
	}
	infos, err := repo.ListUserSessions(ctx, "owner")
	if err != nil || len(infos) != 1 || infos[0].HashedID != "live" || infos[0].UserID != "owner" || infos[0].UserAgent == nil || *infos[0].UserAgent != agent {
		t.Fatalf("owner sessions = %+v, %v", infos, err)
	}
	if err := repo.DeleteUserSessions(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSession(ctx, "live"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("owner session survived: %v", err)
	}
	if got, err := repo.GetSession(ctx, "guest"); err != nil || got.UserID != "guest" {
		t.Fatalf("guest session removed with the owner's: %+v, %v", got, err)
	}
	if err := repo.DeleteSession(ctx, "guest"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSession(ctx, "guest"); err != nil {
		t.Fatalf("deleting a missing session: %v", err)
	}
	if _, err := repo.GetSession(ctx, "guest"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted session survived: %v", err)
	}
}

// testAppTokenExpiry can only observe expiry cleanup through the live token
// it must leave behind: the latest-token lookup already hides expired rows.
func testAppTokenExpiry(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := repo.CreateAppToken(ctx, "stale", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetLatestAppToken(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expired token offered: %v", err)
	}
	live, err := repo.CreateAppToken(ctx, "live", now.Add(time.Hour))
	if err != nil || live.ID == 0 || live.Token != "live" || !live.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("created token = %+v, %v", live, err)
	}
	if err := repo.DeleteExpiredAppTokens(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetLatestAppToken(ctx)
	if err != nil || got.ID != live.ID || got.Token != "live" {
		t.Fatalf("live token lost to expiry cleanup: %+v, %v", got, err)
	}
}
