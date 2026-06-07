package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testTaskUpsertPreservesRuntimeState pins that UpsertTask only writes
// descriptive columns. Runtime counters (last_run_at, last_duration_ms,
// last_status, next_run_at) must survive a redeploy or operators lose run
// history.
func testTaskUpsertPreservesRuntimeState(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	_, err := repo.UpsertTask(ctx, "token_cleanup", "Prune expired tokens", 900)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := repo.MarkTaskRunning(ctx, "token_cleanup"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := repo.MarkTaskSuccess(ctx, "token_cleanup", 1234); err != nil {
		t.Fatalf("mark success: %v", err)
	}
	before, err := repo.GetTask(ctx, "token_cleanup")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if before.LastStatus != repository.TaskStatusSuccess || before.LastDurationMs != 1234 || before.LastRunAt == nil {
		t.Fatalf("setup precondition failed: %+v", before)
	}

	after, err := repo.UpsertTask(ctx, "token_cleanup", "New description", 600)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if after.Description != "New description" || after.IntervalSeconds != 600 {
		t.Errorf("descriptive columns didn't apply: %+v", after)
	}
	if after.LastStatus != repository.TaskStatusSuccess {
		t.Errorf("UpsertTask clobbered last_status: was %q, now %q", before.LastStatus, after.LastStatus)
	}
	if after.LastDurationMs != 1234 {
		t.Errorf("UpsertTask clobbered last_duration_ms: was 1234, now %d", after.LastDurationMs)
	}
	if after.LastRunAt == nil {
		t.Error("UpsertTask clobbered last_run_at to NULL")
	}
}

// testTaskMarkSuccessRearmsNextRun confirms the interval->next_run arithmetic.
// The scheduler's due-list query depends on next_run_at being set so the task
// actually fires again.
func testTaskMarkSuccessRearmsNextRun(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	_, err := repo.UpsertTask(ctx, "eventsub_snapshot", "Poll EventSub", 60)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.MarkTaskSuccess(ctx, "eventsub_snapshot", 50); err != nil {
		t.Fatalf("mark success: %v", err)
	}
	got, err := repo.GetTask(ctx, "eventsub_snapshot")
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

// testTaskQueuedRunSurvivesMarkSuccess pins that a run queued (SetTaskNextRun)
// while a task is active is preserved by MarkTaskSuccess rather than being
// overwritten by the interval rearm.
func testTaskQueuedRunSurvivesMarkSuccess(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.UpsertTask(ctx, "category_metadata_sync", "Fetch category metadata", 24*60*60); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.MarkTaskRunning(ctx, "category_metadata_sync"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := repo.SetTaskNextRun(ctx, "category_metadata_sync"); err != nil {
		t.Fatalf("set next run: %v", err)
	}
	if err := repo.MarkTaskSuccess(ctx, "category_metadata_sync", 50); err != nil {
		t.Fatalf("mark success: %v", err)
	}

	got, err := repo.GetTask(ctx, "category_metadata_sync")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NextRunAt == nil {
		t.Fatal("next_run_at must preserve the queued run")
	}
	if got.NextRunAt.After(time.Now().Add(5 * time.Second)) {
		t.Fatalf("next_run_at = %v, want queued immediate run, not interval rearm", got.NextRunAt)
	}
}

func testTaskSetNextRunMissingReturnsNotFound(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if err := repo.SetTaskNextRun(ctx, "missing-task"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("SetTaskNextRun error = %v, want ErrNotFound", err)
	}
}
