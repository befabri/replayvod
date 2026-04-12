package pgadapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestTask_Upsert_PreservesRuntimeState pins the contract that
// UpsertTask only writes descriptive columns. Runtime counters
// (last_run_at, last_duration_ms, last_status, next_run_at) must
// survive a redeploy or operators lose run history.
func TestTask_Upsert_PreservesRuntimeState(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	_, err := a.UpsertTask(ctx, "token_cleanup", "Prune expired tokens", 900)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
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
// arithmetic on PG (uses NOW() + INTERVAL). Scheduler's due-list query
// depends on this setting so the task actually fires again.
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
	expected := time.Now().Add(60 * time.Second)
	delta := got.NextRunAt.Sub(expected)
	if delta < -5*time.Second || delta > 5*time.Second {
		t.Errorf("next_run_at = %v, want ~%v (delta %v)", got.NextRunAt, expected, delta)
	}
}

// TestEventLog_Append_PreservesJSONData checks the JSONB round-trip.
// A consumer reading the data column as json.RawMessage must see
// semantically-equivalent JSON on the way out.
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

	// PG may reformat JSONB (key-order / whitespace) so compare
	// semantically rather than byte-equal.
	var want, got any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(row.Data, &got); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if wj, _ := json.Marshal(want); string(wj) != mustMarshal(got) {
		t.Errorf("data semantic mismatch: want %v got %v", want, got)
	}
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestEventLog_DeleteOld_SkipsWarnAndError pins the retention contract:
// prune-by-age applies only to debug/info severities. Warn/error rows
// are operationally valuable during incident review and stay longer.
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

	if err := a.DeleteOldEventLogs(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("delete old: %v", err)
	}
	count, err := a.CountEventLogs(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("rows after prune = %d, want 2 (warn + error survive)", count)
	}
}

// TestSettings_Upsert_InsertThenUpdate exercises both UPSERT branches.
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

// TestSettings_UserCascadeDelete confirms ON DELETE CASCADE on
// settings.user_id. FK enforcement is always on in PostgreSQL, unlike
// SQLite where a per-connection pragma is required.
func TestSettings_UserCascadeDelete(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPGPool(t)
	a := New(pggen.New(pool))

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
