package pgadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestSettings_UserCascadeDelete confirms ON DELETE CASCADE on
// settings.user_id. FK enforcement is always on in PostgreSQL, unlike SQLite
// where a per-connection pragma is required — so this stays a per-adapter test.
func TestSettings_UserCascadeDelete(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPGPool(t)
	a := New(pool)

	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: "u-cascade", Login: "u", DisplayName: "u", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := a.UpsertSettings(ctx, &repository.Settings{
		UserID: "u-cascade", Timezone: "UTC", DatetimeFormat: "ISO", Language: "en",
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", "u-cascade"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	_, err := a.GetSettings(ctx, "u-cascade")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("settings row must be cascaded on user delete; got %v", err)
	}
}
