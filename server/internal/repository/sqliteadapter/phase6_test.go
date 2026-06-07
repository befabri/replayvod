package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// testdbSQLiteDB spins up a fresh migrated SQLite file and returns the
// raw handle. Used by cascade tests that need to issue DELETE against
// the parent table directly.
func testdbSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.NewSQLiteDB(t)
}

// TestEventLog_Append_PreservesJSONData checks the JSON round-trip on the data
// column. SQLite stores TEXT, so unlike the Postgres JSONB path this asserts
// byte-for-byte equality: a structured-logging caller needs the bytes back
// unchanged so downstream JSON consumers don't silently get a string-of-bytes.
// The semantic round-trip is covered backend-agnostically by the contract
// suite (WebhookEvent_PayloadRoundTrip); this pins SQLite's stronger
// byte-exact guarantee.
func TestEventLog_Append_PreservesJSONData(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	data := json.RawMessage(`{"schedule_id":42,"job_id":"abc-123"}`)
	row, err := a.CreateEventLog(ctx, &repository.EventLogInput{
		Domain:    "schedule",
		EventType: "auto_download_triggered",
		Severity:  repository.EventLogSeverityInfo,
		Message:   "schedule fired",
		Data:      data,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.CreatedAt.IsZero() {
		t.Error("created_at must be populated by DB default")
	}
	if string(row.Data) != string(data) {
		t.Errorf("data round-trip differs: want %q got %q", string(data), string(row.Data))
	}
}

// TestSettings_UserCascadeDelete confirms the ON DELETE CASCADE on
// settings.user_id. Removing a user must clean up their settings row, not
// leave an orphan. Uses the raw *sql.DB from testdb because the adapter
// deliberately doesn't expose a DeleteUser — cascade is a schema concern,
// exercised through a direct SQL statement. SQLite-specific because FK
// enforcement is per-connection pragma (PostgreSQL always enforces).
func TestSettings_UserCascadeDelete(t *testing.T) {
	ctx := context.Background()
	db := testdbSQLiteDB(t)
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

	// Belt-and-suspenders: DSN-level _foreign_keys=ON applies per connection;
	// assert the pragma took before relying on cascade behavior.
	var fkOn int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fkOn); err != nil {
		t.Fatalf("check fk pragma: %v", err)
	}
	if fkOn != 1 {
		t.Skipf("PRAGMA foreign_keys=%d; FK enforcement off on this SQLite build — cascade can't fire", fkOn)
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", "u-cascade"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	_, err := a.GetSettings(ctx, "u-cascade")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("settings row must be cascaded on user delete; got %v", err)
	}
}
