package contracttest

import (
	"context"
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
