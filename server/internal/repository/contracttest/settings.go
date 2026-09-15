package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testSettingsUpsertInsertThenUpdate exercises both UPSERT branches and pins
// that created_at is preserved across the update.
func testSettingsUpsertInsertThenUpdate(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID: "u-settings", Login: "u", DisplayName: "u", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	created, err := repo.UpsertSettings(ctx, &repository.Settings{
		UserID: "u-settings", Timezone: "Europe/Paris", DatetimeFormat: "EU", Language: "fr",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if created.Timezone != "Europe/Paris" || created.Language != "fr" {
		t.Errorf("insert values not applied: %+v", created)
	}

	updated, err := repo.UpsertSettings(ctx, &repository.Settings{
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

// testEventLogDeleteOldSkipsWarnAndError pins the retention contract:
// prune-by-age applies only to debug/info severities. Warn/error rows are
// operationally valuable during incident review and stay longer.
func testEventLogDeleteOldSkipsWarnAndError(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	for _, sev := range []string{
		repository.EventLogSeverityDebug,
		repository.EventLogSeverityInfo,
		repository.EventLogSeverityWarn,
		repository.EventLogSeverityError,
	} {
		if _, err := repo.CreateEventLog(ctx, &repository.EventLogInput{
			Domain: "test", EventType: "t", Severity: sev, Message: sev,
		}); err != nil {
			t.Fatalf("seed %s: %v", sev, err)
		}
	}

	if err := repo.DeleteOldEventLogs(ctx, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("delete old with a past cutoff: %v", err)
	}
	if count, err := repo.CountEventLogs(ctx); err != nil || count != 4 {
		t.Fatalf("rows after a past cutoff = %d, %v, want 4", count, err)
	}
	if err := repo.DeleteOldEventLogs(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("delete old: %v", err)
	}
	count, err := repo.CountEventLogs(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("rows after prune = %d, want 2 (warn + error survive)", count)
	}
}

// testStorageIdentityRoundTripAndIsolation pins the two storage writes: each
// creates the settings row when none exists, only touches its own column, and
// the scan cursor survives an identity write (and the reverse).
func testStorageIdentityRoundTripAndIsolation(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.GetServerSettings(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("fresh settings err = %v, want ErrNotFound", err)
	}
	if err := repo.SetStorageScanCursor(ctx, 640); err != nil {
		t.Fatalf("SetStorageScanCursor: %v", err)
	}
	created, err := repo.SetStorageID(ctx, "aa11")
	if err != nil {
		t.Fatalf("SetStorageID: %v", err)
	}
	if created.StorageID != "aa11" || created.StorageScanCursor != 640 {
		t.Fatalf("after identity write: id=%q cursor=%d, want aa11/640", created.StorageID, created.StorageScanCursor)
	}
	if _, err := repo.SetSchedulesPaused(ctx, true); err != nil {
		t.Fatalf("SetSchedulesPaused: %v", err)
	}
	if err := repo.SetStorageScanCursor(ctx, 0); err != nil {
		t.Fatalf("SetStorageScanCursor reset: %v", err)
	}
	got, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if got.StorageID != "aa11" || got.StorageScanCursor != 0 || !got.SchedulesPaused {
		t.Fatalf("settings = id %q cursor %d paused %v, want aa11/0/true", got.StorageID, got.StorageScanCursor, got.SchedulesPaused)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created_at moved on a column write: %v -> %v", created.CreatedAt, got.CreatedAt)
	}
}

// The restore phase has its own durable position and must not reset normal
// scan progress or change the last operator settings edit timestamp.
func testStorageRestoreCursorRoundTripAndIsolation(t *testing.T, h Harness) {
	ctx := t.Context()
	repo := h.Repo()
	cursor := int64(0)
	if err := repo.SetStorageRestoreCursor(ctx, &cursor); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetServerSettings(ctx)
	if err != nil || before.StorageRestoreCursor == nil || *before.StorageRestoreCursor != 0 {
		t.Fatalf("created restore cursor = %+v, %v", before, err)
	}
	if err := repo.SetStorageScanCursor(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetStorageID(ctx, "identity"); err != nil {
		t.Fatal(err)
	}
	cursor = 71
	if err := repo.SetStorageRestoreCursor(ctx, &cursor); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetServerSettings(ctx)
	if err != nil || got.StorageRestoreCursor == nil || *got.StorageRestoreCursor != 71 || got.StorageScanCursor != 42 || got.StorageID != "identity" {
		t.Fatalf("cursor update changed other settings: %+v, %v", got, err)
	}
	if !before.CreatedAt.Equal(got.CreatedAt) || !before.UpdatedAt.Equal(got.UpdatedAt) {
		t.Fatal("storage bookkeeping changed settings timestamps")
	}
	cursor = -1
	if err := repo.SetStorageRestoreCursor(ctx, &cursor); err == nil {
		t.Fatal("accepted negative restore cursor")
	}
	if err := repo.SetStorageRestoreCursor(ctx, nil); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetServerSettings(ctx)
	if err != nil || got.StorageRestoreCursor != nil || got.StorageScanCursor != 42 || got.StorageID != "identity" {
		t.Fatalf("reset restore cursor = %+v, %v", got, err)
	}
}
