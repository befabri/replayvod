package system_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/system"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestSearchEventLogs_SQLiteFallback pins the fallback contract: on a
// backend that doesn't satisfy FullTextSearcher, SearchEventLogs still
// returns matching rows (substring, case-insensitive, across
// message/event_type/domain) but with Ranked=false so the UI knows not
// to render relevance scores.
func TestSearchEventLogs_SQLiteFallback(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)

	// sanity: SQLite adapter MUST NOT satisfy FullTextSearcher — the
	// whole point of the fallback is that this assertion fails.
	if _, ok := any(repo).(repository.FullTextSearcher); ok {
		t.Fatalf("SQLiteAdapter unexpectedly satisfies FullTextSearcher")
	}

	seed := []repository.EventLogInput{
		{Domain: "download", EventType: "failed", Severity: "error",
			Message: "download failed: network timeout"},
		{Domain: "auth", EventType: "refresh", Severity: "info",
			Message: "token refreshed"},
		{Domain: "schedule", EventType: "triggered", Severity: "info",
			Message: "triggered download for broadcaster foo"},
	}
	for i := range seed {
		if _, err := repo.CreateEventLog(ctx, &seed[i]); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("message substring match", func(t *testing.T) {
		got, err := svc.SearchEventLogs(ctx, "timeout", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if got.Ranked {
			t.Fatalf("fallback path must set Ranked=false")
		}
		if got.Total != 1 || len(got.Results) != 1 {
			t.Fatalf("want 1 match, got total=%d len=%d", got.Total, len(got.Results))
		}
		if got.Results[0].EventType != "failed" {
			t.Fatalf("wrong row matched: %+v", got.Results[0])
		}
	})

	t.Run("case-insensitive domain match", func(t *testing.T) {
		got, err := svc.SearchEventLogs(ctx, "AUTH", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if got.Total != 1 {
			t.Fatalf("expected 1 auth match, got %d", got.Total)
		}
	})

	t.Run("empty query returns empty, not all rows", func(t *testing.T) {
		got, err := svc.SearchEventLogs(ctx, "", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if got.Total != 0 || len(got.Results) != 0 {
			t.Fatalf("empty query must not return rows: total=%d len=%d", got.Total, len(got.Results))
		}
	})

	t.Run("pagination slices matched set", func(t *testing.T) {
		// "download" hits 2 rows (event_type=download on row 0's message
		// + domain=download on row 0 + schedule message "triggered
		// download").
		all, err := svc.SearchEventLogs(ctx, "download", 10, 0)
		if err != nil {
			t.Fatalf("all: %v", err)
		}
		if all.Total < 2 {
			t.Fatalf("need ≥2 matches for pagination, got %d", all.Total)
		}
		page, err := svc.SearchEventLogs(ctx, "download", 1, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if len(page.Results) != 1 {
			t.Fatalf("expected 1 row, got %d", len(page.Results))
		}
		if page.Results[0].ID != all.Results[1].ID {
			t.Fatalf("offset=1 should return all.Results[1]: want %d got %d",
				all.Results[1].ID, page.Results[0].ID)
		}
	})
}

// TestUpdatePlaybackCacheConfig_RejectsZeroPercent pins that 0% is denied: an
// enabled cache at 0% does nothing (active() needs maxPercent > 0), so the
// service must reject it rather than save a silent no-op config. Valid percents
// round-trip.
func TestUpdatePlaybackCacheConfig_RejectsZeroPercent(t *testing.T) {
	ctx := context.Background()
	svc := system.New(sqliteadapter.New(testdb.NewSQLiteDB(t)), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := svc.UpdatePlaybackCacheConfig(ctx, system.PlaybackCacheConfig{Enabled: true, MaxPercent: 0, AutoGenerate: true}); !errors.Is(err, system.ErrInvalidPlaybackCacheConfig) {
		t.Fatalf("UpdatePlaybackCacheConfig(0%%) err = %v, want ErrInvalidPlaybackCacheConfig", err)
	}

	got, err := svc.UpdatePlaybackCacheConfig(ctx, system.PlaybackCacheConfig{Enabled: true, MaxPercent: 25, AutoGenerate: true})
	if err != nil {
		t.Fatalf("UpdatePlaybackCacheConfig(25%%): %v", err)
	}
	if !got.Enabled || got.MaxPercent != 25 || !got.AutoGenerate {
		t.Fatalf("config = %+v, want enabled/25/auto", got)
	}
}

func TestUpdateUserRole_OwnerCarveOut(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	seed := map[string]string{"owner-1": "owner", "admin-1": "admin", "viewer-1": "viewer"}
	for id, role := range seed {
		if _, err := repo.UpsertUser(ctx, &repository.User{ID: id, Login: id, DisplayName: id, Role: role}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	owner := &repository.User{ID: "owner-1", Role: "owner"}
	admin := &repository.User{ID: "admin-1", Role: "admin"}

	got, err := svc.UpdateUserRole(ctx, admin, "viewer-1", "admin")
	if err != nil {
		t.Fatalf("admin promotes viewer→admin: %v", err)
	}
	if got.Role != "admin" {
		t.Fatalf("role = %q, want admin", got.Role)
	}

	if _, err := svc.UpdateUserRole(ctx, admin, "viewer-1", "owner"); !errors.Is(err, system.ErrOwnerRoleRequired) {
		t.Fatalf("admin grants owner err = %v, want ErrOwnerRoleRequired", err)
	}
	if _, err := svc.UpdateUserRole(ctx, admin, "owner-1", "viewer"); !errors.Is(err, system.ErrOwnerRoleRequired) {
		t.Fatalf("admin demotes owner err = %v, want ErrOwnerRoleRequired", err)
	}

	if _, err := svc.UpdateUserRole(ctx, owner, "viewer-1", "owner"); err != nil {
		t.Fatalf("owner grants owner: %v", err)
	}
	if _, err := svc.UpdateUserRole(ctx, owner, "owner-1", "viewer"); !errors.Is(err, system.ErrCannotDemoteSelf) {
		t.Fatalf("owner self-demote err = %v, want ErrCannotDemoteSelf", err)
	}
}

func TestRemoveFromWhitelistRevokesUserSessions(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	const userID = "user-1"
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: userID, Login: "user1", DisplayName: "User 1", Role: "viewer"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := repo.AddToWhitelist(ctx, userID); err != nil {
		t.Fatalf("add whitelist: %v", err)
	}
	if err := repo.CreateSession(ctx, &repository.Session{
		HashedID:        "session-hash",
		UserID:          userID,
		EncryptedTokens: []byte("tokens"),
		ExpiresAt:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	admin := &repository.User{ID: "admin-1", Role: "admin"}
	if err := svc.RemoveFromWhitelist(ctx, admin, userID); err != nil {
		t.Fatalf("RemoveFromWhitelist: %v", err)
	}
	sessions, err := repo.ListUserSessions(ctx, userID)
	if err != nil {
		t.Fatalf("ListUserSessions after removal: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions after removal = %d, want 0", len(sessions))
	}
}

// TestRemoveFromWhitelist_OwnerCarveOut checks that admins cannot revoke owner
// access.
func TestRemoveFromWhitelist_OwnerCarveOut(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "owner-1", Login: "owner1", DisplayName: "Owner", Role: "owner"}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	for _, id := range []string{"owner-1", "stranger-1"} {
		if err := repo.AddToWhitelist(ctx, id); err != nil {
			t.Fatalf("whitelist %s: %v", id, err)
		}
	}
	if err := repo.CreateSession(ctx, &repository.Session{
		HashedID:        "owner-session",
		UserID:          "owner-1",
		EncryptedTokens: []byte("tokens"),
		ExpiresAt:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed owner session: %v", err)
	}

	admin := &repository.User{ID: "admin-1", Role: "admin"}
	if err := svc.RemoveFromWhitelist(ctx, admin, "owner-1"); !errors.Is(err, system.ErrOwnerRoleRequired) {
		t.Fatalf("admin de-whitelists owner err = %v, want ErrOwnerRoleRequired", err)
	}
	sessions, err := repo.ListUserSessions(ctx, "owner-1")
	if err != nil || len(sessions) != 1 {
		t.Fatalf("owner sessions after denied removal = %d, %v; want 1 intact", len(sessions), err)
	}

	if err := svc.RemoveFromWhitelist(ctx, admin, "stranger-1"); err != nil {
		t.Fatalf("admin removes rowless entry: %v", err)
	}

	owner := &repository.User{ID: "owner-2", Role: "owner"}
	if err := svc.RemoveFromWhitelist(ctx, owner, "owner-1"); err != nil {
		t.Fatalf("owner removes owner entry: %v", err)
	}
}

// whitelistFakeRepo drives RemoveFromWhitelist's two error paths in isolation:
// the whitelist-row removal and the follow-up session revoke fail independently.
type whitelistFakeRepo struct {
	repository.Repository
	removeErr    error
	deleteErr    error
	deleteCalled bool
}

func (f *whitelistFakeRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return fn(f)
}
func (f *whitelistFakeRepo) GetUserForUpdate(context.Context, string) (*repository.User, error) {
	return &repository.User{Role: "viewer"}, nil
}

func (f *whitelistFakeRepo) RemoveFromWhitelist(context.Context, string) error { return f.removeErr }

func (f *whitelistFakeRepo) DeleteUserSessions(context.Context, string) error {
	f.deleteCalled = true
	return f.deleteErr
}

// TestRemoveFromWhitelist_SessionRevokeFailurePropagates pins that a failed
// session revoke does fail the call. The whitelist row delete is idempotent, so
// a retry still reaches DeleteUserSessions; returning success here would give
// the admin a false OK while the user's active sessions remain valid.
func TestRemoveFromWhitelist_SessionRevokeFailurePropagates(t *testing.T) {
	wantErr := errors.New("sessions table locked")
	repo := &whitelistFakeRepo{deleteErr: wantErr}
	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.RemoveFromWhitelist(context.Background(), &repository.User{ID: "owner-1", Role: "owner"}, "user-1"); !errors.Is(err, wantErr) {
		t.Fatalf("RemoveFromWhitelist err = %v, want wrapped %v", err, wantErr)
	}
	if !repo.deleteCalled {
		t.Fatal("session revoke was not attempted")
	}
}

// TestRemoveFromWhitelist_PrimaryRemovalErrorPropagates pins the other side:
// when the whitelist removal itself fails, the error surfaces and sessions are
// NOT revoked (the user is still whitelisted, so revoking would be wrong).
func TestRemoveFromWhitelist_PrimaryRemovalErrorPropagates(t *testing.T) {
	wantErr := errors.New("whitelist write failed")
	repo := &whitelistFakeRepo{removeErr: wantErr}
	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.RemoveFromWhitelist(context.Background(), &repository.User{ID: "owner-1", Role: "owner"}, "user-1"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if repo.deleteCalled {
		t.Fatal("sessions must not be revoked when the whitelist removal failed")
	}
}
