package sqliteadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestSettings_UserCascadeDelete confirms the ON DELETE CASCADE on
// settings.user_id. Nothing in the interface deletes a user, so the cascade
// is exercised with a direct statement on the handle.
func TestSettings_UserCascadeDelete(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	a := New(db)

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

	// The data source name turns foreign keys on for every connection; a
	// cascade that cannot fire is a regression, not a reason to skip.
	var fkOn int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fkOn); err != nil || fkOn != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, %v; want 1", fkOn, err)
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", "u-cascade"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	_, err := a.GetSettings(ctx, "u-cascade")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("settings row must be cascaded on user delete; got %v", err)
	}
}
