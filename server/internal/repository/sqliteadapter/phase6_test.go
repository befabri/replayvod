package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// testdbSQLiteDB spins up a fresh migrated SQLite file and returns the
// raw handle. Used by cascade tests that need to issue DELETE against
// the parent table directly.
func testdbSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.NewSQLiteDB(t)
}

// TestTask_Upsert_PreservesRuntimeState pins the contract that
// UpsertTask only writes the descriptive columns (name, description,
// interval_seconds). Runtime state (last_run_at, last_duration_ms,
// last_status, next_run_at) must survive a redeploy — otherwise
// restarting the binary would reset every counter and operators would
// lose visibility into which tasks actually ran.
func TestTask_Upsert_PreservesRuntimeState(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	_, err := a.UpsertTask(ctx, "token_cleanup", "Prune expired tokens", 900)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Simulate a run: mark running, then mark success with duration.
	if err := a.MarkTaskRunning(ctx, "token_cleanup"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := a.MarkTaskSuccess(ctx, "token_cleanup", 1234); err != nil {
		t.Fatalf("mark success: %v", err)
	}
	before, err := a.GetTask(ctx, "token_cleanup")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if before.LastStatus != repository.TaskStatusSuccess || before.LastDurationMs != 1234 || before.LastRunAt == nil {
		t.Fatalf("setup precondition failed: %+v", before)
	}

	// Redeploy simulation: upsert with a new description + interval.
	after, err := a.UpsertTask(ctx, "token_cleanup", "New description", 600)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if after.Description != "New description" || after.IntervalSeconds != 600 {
		t.Errorf("descriptive columns didn't apply: %+v", after)
	}
	if after.LastStatus != repository.TaskStatusSuccess {
		t.Errorf("UpsertTask clobbered last_status: was %q, now %q",
			before.LastStatus, after.LastStatus)
	}
	if after.LastDurationMs != 1234 {
		t.Errorf("UpsertTask clobbered last_duration_ms: was 1234, now %d", after.LastDurationMs)
	}
	if after.LastRunAt == nil {
		t.Error("UpsertTask clobbered last_run_at to NULL")
	}
}

// TestTask_MarkSuccess_RearmsNextRun confirms the interval→next_run
// arithmetic. After a success, next_run_at must be set to now +
// interval_seconds so the scheduler's due-list query picks the task up
// at the right time. A manual run-now (SetTaskNextRun) is the
// companion path; this test covers the auto-cadence side.
func TestTask_MarkSuccess_RearmsNextRun(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	_, err := a.UpsertTask(ctx, "eventsub_snapshot", "Poll EventSub", 60)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := a.MarkTaskSuccess(ctx, "eventsub_snapshot", 50); err != nil {
		t.Fatalf("mark success: %v", err)
	}
	got, err := a.GetTask(ctx, "eventsub_snapshot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NextRunAt == nil {
		t.Fatal("next_run_at must be set after MarkTaskSuccess on an intervaled task")
	}
	// Allow a window for clock skew between the SQL datetime('now') and
	// the Go clock.
	expected := time.Now().Add(60 * time.Second)
	delta := got.NextRunAt.Sub(expected)
	if delta < -5*time.Second || delta > 5*time.Second {
		t.Errorf("next_run_at = %v, want ~%v (delta %v)", got.NextRunAt, expected, delta)
	}
}

// TestEventLog_Append_PreservesJSONData checks the JSON round-trip on
// the data column (SQLite stores TEXT; PG uses JSONB in its mirror).
// A structured-logging caller writing a field like `{"stream_id":"..."}`
// needs bytes back byte-equivalent so downstream JSON consumers don't
// silently get a string-of-bytes.
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

// TestEventLog_DeleteOld_SkipsWarnAndError pins the retention contract:
// prune by age applies only to debug/info severities. Warn/error rows
// are operationally valuable during incident review and stay longer.
// The retention task's cutoff for those uses a different query.
func TestEventLog_DeleteOld_SkipsWarnAndError(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	for _, sev := range []string{
		repository.EventLogSeverityDebug,
		repository.EventLogSeverityInfo,
		repository.EventLogSeverityWarn,
		repository.EventLogSeverityError,
	} {
		if _, err := a.CreateEventLog(ctx, &repository.EventLogInput{
			Domain: "test", EventType: "t", Severity: sev, Message: sev,
		}); err != nil {
			t.Fatalf("seed %s: %v", sev, err)
		}
	}

	// Prune everything that's "older than the future" — i.e. all rows.
	if err := a.DeleteOldEventLogs(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("delete old: %v", err)
	}
	count, err := a.CountEventLogs(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	// Debug + info swept; warn + error retained.
	if count != 2 {
		t.Errorf("rows after prune = %d, want 2 (warn + error survive)", count)
	}
}

// TestSettings_Upsert_InsertThenUpdate exercises both branches of the
// UPSERT. First call inserts with the user defaults; second call with
// different values updates in place.
func TestSettings_Upsert_InsertThenUpdate(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: "u-settings", Login: "u", DisplayName: "u", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	created, err := a.UpsertSettings(ctx, &repository.Settings{
		UserID: "u-settings", Timezone: "Europe/Paris", DatetimeFormat: "EU", Language: "fr",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if created.Timezone != "Europe/Paris" || created.Language != "fr" {
		t.Errorf("insert values not applied: %+v", created)
	}

	updated, err := a.UpsertSettings(ctx, &repository.Settings{
		UserID: "u-settings", Timezone: "America/New_York", DatetimeFormat: "US", Language: "en",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Timezone != "America/New_York" || updated.Language != "en" {
		t.Errorf("update values not applied: %+v", updated)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("created_at changed on update: was %v, now %v", created.CreatedAt, updated.CreatedAt)
	}
}

// TestSettings_UserCascadeDelete confirms the ON DELETE CASCADE on
// settings.user_id. Removing a user must clean up their settings row,
// not leave an orphan that would FK-violate a later user with the
// same ID. Uses the raw *sql.DB from testdb because the adapter
// deliberately doesn't expose a DeleteUser — cascade is a schema
// concern, exercised through a direct SQL statement.
func TestSettings_UserCascadeDelete(t *testing.T) {
	ctx := context.Background()
	db := testdbSQLiteDB(t)
	a := New(sqlitegen.New(db))

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
