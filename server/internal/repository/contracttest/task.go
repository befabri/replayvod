package contracttest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testTaskUpsertPreservesRuntimeState(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	_, err := repo.UpsertTask(ctx, "token_cleanup", "Prune expired tokens", 900)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := repo.ClaimTask(ctx, "token_cleanup", "execution"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := repo.SettleTask(ctx, "token_cleanup", "execution", repository.TaskStatusSuccess, 1234, ""); err != nil {
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

func testTaskMarkSuccessRearmsNextRun(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	_, err := repo.UpsertTask(ctx, "eventsub_snapshot", "Poll EventSub", 60)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.ClaimTask(ctx, "eventsub_snapshot", "execution"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "eventsub_snapshot", "execution", repository.TaskStatusSuccess, 50, ""); err != nil {
		t.Fatalf("mark success: %v", err)
	}
	got, err := repo.GetTask(ctx, "eventsub_snapshot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NextRunAt == nil {
		t.Fatal("next_run_at must be set after SettleTask on an intervaled task")
	}
	expected := time.Now().Add(60 * time.Second)
	delta := got.NextRunAt.Sub(expected)
	if delta < -5*time.Second || delta > 5*time.Second {
		t.Errorf("next_run_at = %v, want ~%v (delta %v)", got.NextRunAt, expected, delta)
	}
}

func testTaskQueuedRunSurvivesMarkSuccess(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.UpsertTask(ctx, "category_metadata_sync", "Fetch category metadata", 24*60*60); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.ClaimTask(ctx, "category_metadata_sync", "execution"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := repo.SetTaskNextRun(ctx, "category_metadata_sync"); err != nil {
		t.Fatalf("set next run: %v", err)
	}
	if err := repo.SettleTask(ctx, "category_metadata_sync", "execution", repository.TaskStatusSuccess, 50, ""); err != nil {
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

func testTaskInterruptedRetriesImmediately(t *testing.T, h Harness) {
	repo := h.Repo()
	ctx := context.Background()
	if _, err := repo.UpsertTask(ctx, "interrupt", "daily", 86400); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "interrupt", "prior"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "interrupt", "prior", repository.TaskStatusFailed, 2, "old failure"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetTaskNextRun(ctx, "interrupt"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "interrupt", "execution"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "interrupt", "execution", repository.TaskStatusInterrupted, 37, ""); err != nil {
		t.Fatal(err)
	}
	row, err := repo.GetTask(ctx, "interrupt")
	if err != nil {
		t.Fatal(err)
	}
	if row.LastStatus != repository.TaskStatusInterrupted || row.LastError != nil || row.LastDurationMs != 37 || row.NextRunAt == nil || time.Until(*row.NextRunAt) > time.Second {
		t.Fatalf("interrupted result: %+v", row)
	}
	due, err := repo.ListDueTasks(ctx)
	if err != nil || len(due) != 1 || due[0].Name != "interrupt" {
		t.Fatalf("interrupted task not due: %+v %v", due, err)
	}
	if _, err := repo.SetTaskEnabled(ctx, "interrupt", false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "interrupt", "execution", repository.TaskStatusInterrupted, 38, ""); err != nil {
		t.Fatal(err)
	}
	due, err = repo.ListDueTasks(ctx)
	if err != nil || len(due) != 0 {
		t.Fatalf("interruption re-enabled a paused task: %+v %v", due, err)
	}
}

func testTaskAutomaticRunRespectsDisabledState(t *testing.T, h Harness) {
	repo := h.Repo()
	ctx := context.Background()
	if err := repo.ScheduleTaskIfEnabled(ctx, "absent"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	for _, interval := range []int64{0, 60} {
		name := fmt.Sprint(interval)
		if _, err := repo.UpsertTask(ctx, name, "scan", interval); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SetTaskEnabled(ctx, name, false); err != nil {
			t.Fatal(err)
		}
		before, err := repo.GetTask(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.ScheduleTaskIfEnabled(ctx, name); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("disabled: %v", err)
		}
		after, err := repo.GetTask(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("disabled task changed: before=%+v after=%+v", before, after)
		}
		if _, err := repo.SetTaskEnabled(ctx, name, true); err != nil {
			t.Fatal(err)
		}
		err = repo.ScheduleTaskIfEnabled(ctx, name)
		if interval == 0 && !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("zero interval: %v", err)
		}
		if interval > 0 && err != nil {
			t.Fatal(err)
		}
	}
}

func testTaskExplicitRunWithoutInterval(t *testing.T, h Harness) {
	repo, ctx := h.Repo(), t.Context()
	if _, err := repo.UpsertTask(ctx, "manual", "Explicit runs only", 0); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "manual", "unrequested"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("unrequested claim: %v", err)
	}
	if err := repo.SetTaskNextRun(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	due, err := repo.ListDueTasks(ctx)
	if err != nil || len(due) != 1 || due[0].Name != "manual" {
		t.Fatalf("explicit task undiscoverable: %+v %v", due, err)
	}
	if err := repo.ClaimTask(ctx, "manual", "first"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "manual", "first", repository.TaskStatusSuccess, 1, ""); err != nil {
		t.Fatal(err)
	}
	due, err = repo.ListDueTasks(ctx)
	if err != nil || len(due) != 0 {
		t.Fatalf("one-shot task rearmed: %+v %v", due, err)
	}
}

func testRecoverInterruptedTasks(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	tasks := []struct {
		name     string
		interval int64
	}{{"recover-interval", 60}, {"recover-oneshot", 0}, {"recover-idle", 60}, {"recover-paused", 60}}
	register := func() {
		t.Helper()
		for _, task := range tasks {
			if _, err := repo.UpsertTask(ctx, task.name, "recovery", task.interval); err != nil {
				t.Fatal(err)
			}
		}
	}
	register()
	if err := repo.ClaimTask(ctx, "recover-interval", "run-interval"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetTaskNextRun(ctx, "recover-oneshot"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "recover-oneshot", "run-oneshot"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "recover-idle", "run-idle"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "recover-idle", "run-idle", repository.TaskStatusSuccess, 5, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "recover-paused", "run-paused"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetTaskEnabled(ctx, "recover-paused", false); err != nil {
		t.Fatal(err)
	}
	idleBefore, err := repo.GetTask(ctx, "recover-idle")
	if err != nil || idleBefore.NextRunAt == nil {
		t.Fatalf("settled task = %+v, %v", idleBefore, err)
	}
	if err := repo.ResetTaskAvailability(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecoverInterruptedTasks(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"recover-interval", "recover-oneshot", "recover-paused"} {
		row, err := repo.GetTask(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if row.LastStatus != repository.TaskStatusInterrupted || row.NextRunAt == nil || time.Until(*row.NextRunAt) > time.Second || row.ExecutionID != "run-"+strings.TrimPrefix(name, "recover-") || row.LastError != nil || row.IsAvailable {
			t.Fatalf("recovered %s = %+v", name, row)
		}
	}
	idle, err := repo.GetTask(ctx, "recover-idle")
	if err != nil || idle.LastStatus != repository.TaskStatusSuccess || idle.NextRunAt == nil || !idle.NextRunAt.Equal(*idleBefore.NextRunAt) {
		t.Fatalf("recovery touched a settled task: %+v, %v", idle, err)
	}
	recovered, err := repo.GetTask(ctx, "recover-interval")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecoverInterruptedTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if again, err := repo.GetTask(ctx, "recover-interval"); err != nil || again.LastStatus != repository.TaskStatusInterrupted || !again.NextRunAt.Equal(*recovered.NextRunAt) {
		t.Fatalf("repeated recovery moved the retry: %+v, %v", again, err)
	}
	register()
	due, err := repo.ListDueTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(due))
	for i, task := range due {
		names[i] = task.Name
	}
	slices.Sort(names)
	if want := []string{"recover-interval", "recover-oneshot"}; !slices.Equal(names, want) {
		t.Fatalf("due after recovery = %v, want %v", names, want)
	}
	if err := repo.ClaimTask(ctx, "recover-oneshot", "run-oneshot-2"); err != nil {
		t.Fatalf("interrupted one-shot not claimable: %v", err)
	}
}
