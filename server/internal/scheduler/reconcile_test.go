package scheduler

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestReconcileTasksSQLite(t *testing.T) {
	testReconcileTasks(t, newTestRepo)
}

func testReconcileTasks(t *testing.T, newRepo func(*testing.T) repository.Repository) {
	t.Helper()
	run := func(context.Context) error { t.Error("reconciliation executed a task"); return nil }
	for _, state := range []string{"available", "paused", "unavailable", "queued-again"} {
		t.Run("recover abandoned one-shot/"+state, func(t *testing.T) {
			repo, ctx := newRepo(t), t.Context()
			registry := newTestRegistry(t, Task{Name: "one-shot", Run: run})
			if err := ReconcileTasks(ctx, repo, registry); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetTaskNextRun(ctx, "one-shot"); err != nil {
				t.Fatal(err)
			}
			if err := repo.ClaimTask(ctx, "one-shot", "abandoned-execution"); err != nil {
				t.Fatal(err)
			}
			if state == "paused" {
				if _, err := repo.SetTaskEnabled(ctx, "one-shot", false); err != nil {
					t.Fatal(err)
				}
			}
			if state == "queued-again" {
				if err := repo.SetTaskNextRun(ctx, "one-shot"); err != nil {
					t.Fatal(err)
				}
			}
			before, err := repo.GetTask(ctx, "one-shot")
			if err != nil {
				t.Fatal(err)
			}
			bootRegistry := registry
			if state == "unavailable" {
				bootRegistry = Registry{}
			}
			for range 2 {
				if err := ReconcileTasks(ctx, repo, bootRegistry); err != nil {
					t.Fatal(err)
				}
			}
			row, err := repo.GetTask(ctx, "one-shot")
			if err != nil || row.LastStatus != repository.TaskStatusInterrupted || row.NextRunAt == nil || row.ExecutionID != before.ExecutionID || !reflect.DeepEqual(row.LastRunAt, before.LastRunAt) {
				t.Fatalf("boot did not preserve and recover the abandoned run: %+v, %v", row, err)
			}
			if before.NextRunAt != nil && !row.NextRunAt.Equal(*before.NextRunAt) {
				t.Fatal("recovery replaced a queued run request")
			}
			if err := repo.SettleTask(ctx, "one-shot", "abandoned-execution", repository.TaskStatusSuccess, 10, ""); !errors.Is(err, repository.ErrStaleExecution) {
				t.Fatalf("abandoned execution overwrote recovery: %v", err)
			}
			if state == "paused" || state == "unavailable" {
				if due, err := repo.ListDueTasks(ctx); err != nil || len(due) != 0 {
					t.Fatalf("recovery enabled a paused or unavailable task: %+v, %v", due, err)
				}
			}
			if err := ReconcileTasks(ctx, repo, registry); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SetTaskEnabled(ctx, "one-shot", true); err != nil {
				t.Fatal(err)
			}
			if due, err := repo.ListDueTasks(ctx); err != nil || len(due) != 1 {
				t.Fatalf("recovered one-shot is not eligible: %+v, %v", due, err)
			}
			if err := repo.ClaimTask(ctx, "one-shot", "new-execution"); err != nil {
				t.Fatal(err)
			}
			if err := repo.SettleTask(ctx, "one-shot", "new-execution", repository.TaskStatusSuccess, 10, ""); err != nil {
				t.Fatal(err)
			}
			if due, err := repo.ListDueTasks(ctx); err != nil || len(due) != 0 {
				t.Fatalf("settled one-shot was scheduled again: %+v, %v", due, err)
			}
		})
	}
	t.Run("metadata and availability preserve durable state", func(t *testing.T) {
		repo, ctx := newRepo(t), t.Context()
		for _, name := range []string{"retained", "removed"} {
			if _, err := repo.UpsertTask(ctx, name, "previous description", 60); err != nil {
				t.Fatal(err)
			}
			if err := repo.ClaimTask(ctx, name, "previous-execution"); err != nil {
				t.Fatal(err)
			}
			if err := repo.SettleTask(ctx, name, "previous-execution", repository.TaskStatusFailed, 42, "previous failure"); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SetTaskEnabled(ctx, name, false); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetTaskNextRun(ctx, name); err != nil {
				t.Fatal(err)
			}
		}
		before, err := repo.ListTasks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		registry := newTestRegistry(t,
			Task{Name: "retained", Description: "current description", IntervalSeconds: 120, Run: run},
			Task{Name: "new-manual", Description: "new task", Run: run},
		)
		// Repeated startup and an intervening global disablement preserve both
		// paused tasks' history and their queued run requests.
		for _, next := range []Registry{registry, registry, {}, registry} {
			if err := ReconcileTasks(ctx, repo, next); err != nil {
				t.Fatal(err)
			}
			for _, previous := range before {
				row, err := repo.GetTask(ctx, previous.Name)
				if err != nil {
					t.Fatal(err)
				}
				want := previous
				want.IsAvailable = next.Len() > 0 && want.Name == "retained"
				if want.Name == "retained" {
					want.Description, want.IntervalSeconds = "current description", 120
				}
				want.UpdatedAt = row.UpdatedAt
				if !reflect.DeepEqual(want, *row) {
					t.Fatalf("reconciliation changed durable state\n got: %+v\nwant: %+v", *row, want)
				}
			}
		}
		row, err := repo.GetTask(ctx, "new-manual")
		if err != nil || !row.IsAvailable || !row.IsEnabled || row.IntervalSeconds != 0 || row.LastRunAt != nil || row.NextRunAt != nil {
			t.Fatalf("new task defaults: %+v, %v", row, err)
		}
		if due, err := repo.ListDueTasks(ctx); err != nil || len(due) != 0 {
			t.Fatalf("paused or unrequested manual tasks became due: %+v, %v", due, err)
		}
	})

	for _, failure := range []string{"reset", "upsert", "cancellation"} {
		t.Run("rollback/"+failure, func(t *testing.T) {
			repo := newRepo(t)
			for _, name := range []string{"a-existing", "removed"} {
				if _, err := repo.UpsertTask(t.Context(), name, "old", 60); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.ClaimTask(t.Context(), "a-existing", "abandoned-execution"); err != nil {
				t.Fatal(err)
			}
			before, err := repo.ListTasks(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			registry := newTestRegistry(t,
				Task{Name: "a-existing", Description: "changed", IntervalSeconds: 120, Run: run},
				Task{Name: "b-new", IntervalSeconds: 60, Run: run},
			)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			boom := errors.New("injected reconciliation failure")
			fault := &reconcileFailureRepo{Repository: repo, failName: "b-new", err: boom}
			if failure == "reset" {
				fault.failReset = true
			}
			if failure == "cancellation" {
				fault.cancel, fault.err = cancel, context.Canceled
			}
			if err := ReconcileTasks(ctx, fault, registry); !errors.Is(err, fault.err) {
				t.Fatalf("error = %v, want %v", err, fault.err)
			}
			after, err := repo.ListTasks(t.Context())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed reconciliation partially committed\nbefore: %+v\nafter: %+v\nerror: %v", before, after, err)
			}
			if err := ReconcileTasks(t.Context(), repo, registry); err != nil {
				t.Fatalf("retry after rollback: %v", err)
			}
			row, err := repo.GetTask(t.Context(), "b-new")
			if err != nil || !row.IsAvailable {
				t.Fatalf("retry did not publish registry: %+v, %v", row, err)
			}
		})
	}
}

// reconcileFailureRepo fails after real transaction writes to exercise full rollback.
type reconcileFailureRepo struct {
	repository.Repository
	failReset bool
	failName  string
	err       error
	cancel    context.CancelFunc
}

func (r *reconcileFailureRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		fault := *r
		fault.Repository = tx
		return fn(&fault)
	})
}

func (r *reconcileFailureRepo) ResetTaskAvailability(ctx context.Context) error {
	if err := r.Repository.ResetTaskAvailability(ctx); err != nil {
		return err
	}
	if r.failReset {
		return r.err
	}
	return nil
}

func (r *reconcileFailureRepo) UpsertTask(ctx context.Context, name, description string, seconds int64) (*repository.Task, error) {
	row, err := r.Repository.UpsertTask(ctx, name, description, seconds)
	if err != nil {
		return nil, err
	}
	if name == r.failName {
		if r.cancel != nil {
			r.cancel()
		}
		return nil, r.err
	}
	return row, nil
}
