package contracttest

import (
	"errors"
	"reflect"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testPlaybackCacheConfigIsolation(t *testing.T, h Harness) {
	repo, ctx := h.Repo(), t.Context()
	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "relay", EventSubRelayIngestURL: "https://relay.example/ingest"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureServerHMACSecret(ctx, "eventsub-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertRecordingWebhookConfig(ctx, true, "https://receiver.example/webhook", "recording.completed"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSchedulesPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		row, err := repo.UpsertPlaybackCacheConfig(ctx, enabled, 37, !enabled)
		if err != nil {
			t.Fatal(err)
		}
		if row.PlaybackCacheEnabled != enabled || row.PlaybackCacheAutoGenerate != !enabled || row.PlaybackCacheMaxPercent != 37 {
			t.Fatalf("cache fields swapped: %+v", row)
		}
		loaded, err := repo.GetServerSettings(ctx)
		if err != nil || !reflect.DeepEqual(row, loaded) {
			t.Fatalf("cache config not persisted: %+v, %v", loaded, err)
		}
		row.PlaybackCacheEnabled, row.PlaybackCacheAutoGenerate, row.PlaybackCacheMaxPercent, row.UpdatedAt = before.PlaybackCacheEnabled, before.PlaybackCacheAutoGenerate, before.PlaybackCacheMaxPercent, before.UpdatedAt
		if !reflect.DeepEqual(row, before) {
			t.Fatalf("cache configuration clobbered unrelated settings: %+v", row)
		}
		if secret, err := repo.GetServerHMACSecret(ctx); err != nil || secret != "eventsub-secret" {
			t.Fatal("cache configuration clobbered EventSub secret")
		}
	}
}

func testTaskAvailabilityGuards(t *testing.T, h Harness) {
	repo, ctx := h.Repo(), t.Context()
	if _, err := repo.UpsertTask(ctx, "configured", "task", 60); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetTaskAvailability(ctx); err != nil {
		t.Fatal(err)
	}
	if due, err := repo.ListDueTasks(ctx); err != nil || len(due) != 0 {
		t.Fatalf("unavailable task due: %+v, %v", due, err)
	}
	if err := repo.ClaimTask(ctx, "configured", "unavailable"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("unavailable task claimed: %v", err)
	}
	if err := repo.SetTaskNextRun(ctx, "configured"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unavailable manual run queued: %v", err)
	}
	if row, err := repo.UpsertTask(ctx, "configured", "task", 60); err != nil || !row.IsAvailable {
		t.Fatalf("re-registration failed: %+v, %v", row, err)
	}
	if err := repo.ClaimTask(ctx, "configured", "available"); err != nil {
		t.Fatal(err)
	}
}
