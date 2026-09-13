package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

func newTestRepo(t *testing.T) repository.Repository {
	t.Helper()
	return sqliteadapter.New(testdb.NewSQLiteDB(t))
}

func newTestRegistry(t *testing.T, tasks ...Task) Registry {
	t.Helper()
	registry, err := NewRegistry(tasks)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func newTestScheduler(t *testing.T, tasks ...Task) (*Service, repository.Repository) {
	t.Helper()
	repo := newTestRepo(t)
	registry := newTestRegistry(t, tasks...)
	if err := ReconcileTasks(t.Context(), repo, registry); err != nil {
		t.Fatal(err)
	}
	s := NewService(repo, registry, slog.New(slog.DiscardHandler), 20*time.Millisecond, nil)
	t.Cleanup(s.Stop)
	return s, repo
}

func TestScheduler_RunsDueTask_AndMarksSuccess(t *testing.T) {
	var runs atomic.Int32
	s, repo := newTestScheduler(t, Task{
		Name: "ran", Description: "test", IntervalSeconds: 3600,
		Run: func(context.Context) error {
			runs.Add(1)
			return nil
		},
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		row, err := repo.GetTask(t.Context(), "ran")
		return err == nil && row.LastStatus == repository.TaskStatusSuccess
	})
	if runs.Load() != 1 {
		t.Fatalf("runs = %d, want 1", runs.Load())
	}

	got, err := repo.GetTask(context.Background(), "ran")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastStatus != repository.TaskStatusSuccess {
		t.Errorf("last_status = %q, want success", got.LastStatus)
	}
	if got.NextRunAt == nil {
		t.Error("next_run_at must be set after success on intervaled task")
	}
}

func TestScheduler_TaskFailure_MarksFailedAndContinues(t *testing.T) {
	s, repo := newTestScheduler(t, Task{
		Name: "breaks", Description: "test", IntervalSeconds: 3600,
		Run: func(context.Context) error {
			return errors.New("kaboom")
		},
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		got, err := repo.GetTask(context.Background(), "breaks")
		if err != nil {
			return false
		}
		return got.LastStatus == repository.TaskStatusFailed
	})

	got, err := repo.GetTask(context.Background(), "breaks")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastError == nil || *got.LastError != "kaboom" {
		t.Errorf("last_error = %v, want 'kaboom'", got.LastError)
	}
}

func TestBuildStandardTasks_EventSubOffPreservesTaskPreferences(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			repo := newTestRepo(t)
			ctx := t.Context()
			names := []string{taskEventSubReconcileChannels, taskEventSubSnapshot}
			for _, name := range names {
				if _, err := repo.UpsertTask(ctx, name, "previous EventSub task", 60); err != nil {
					t.Fatal(err)
				}
				if _, err := repo.SetTaskEnabled(ctx, name, enabled); err != nil {
					t.Fatal(err)
				}
				if err := repo.SetTaskNextRun(ctx, name); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{
				App:        config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, EventsubIntervalMinutes: 10, EventsubReconcileIntervalMinutes: 15}},
				ServerMode: config.ServerModeConfig{Mode: config.ServerModeOff},
			}
			registry := newTestRegistry(t, BuildStandardTasks(cfg, repo, StandardTaskDeps{EventSub: eventsubService(t)}, slog.New(slog.DiscardHandler))...)
			if err := ReconcileTasks(ctx, repo, registry); err != nil {
				t.Fatal(err)
			}
			for _, name := range names {
				if row, err := repo.GetTask(ctx, name); err != nil || row.IsAvailable || row.IsEnabled != enabled {
					t.Fatalf("unavailable task %s lost preferences: %+v, %v", name, row, err)
				}
			}
			due, err := repo.ListDueTasks(ctx)
			if err != nil || len(due) != 0 {
				t.Fatalf("obsolete handlers remained due: %+v %v", due, err)
			}
		})
	}
}

func TestBuildStandardTasks_EventSubActiveRegistersIntervaledTasks(t *testing.T) {
	repo := newTestRepo(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tc := twitch.NewClient("client-id", "client-secret", log)
	esvc := eventsub.New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "0123456789abcdef", log)

	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				Enabled:                          true,
				EventsubReconcileIntervalMinutes: 15,
				EventsubIntervalMinutes:          15,
			},
		},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
	}

	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{EventSub: esvc}, log)

	// Starting the scheduler would contact Twitch with this real client.
	for _, name := range []string{taskEventSubReconcileChannels, taskEventSubSnapshot} {
		task, ok := tasks[name]
		if !ok {
			t.Fatalf("%s was not registered in the active branch", name)
		}
		if task.IntervalSeconds != 15*60 {
			t.Fatalf("%s interval = %d, want 900 (15m), not the disabled 0", name, task.IntervalSeconds)
		}
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}

func TestScheduler_Stop_CancelsRunningTask(t *testing.T) {
	started := make(chan struct{})
	s, repo := newTestScheduler(t, Task{
		Name: "long", Description: "test", IntervalSeconds: 3600,
		Run: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task never started")
	}
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return once the task was cancelled")
	}
	got, err := repo.GetTask(context.Background(), "long")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastStatus != repository.TaskStatusInterrupted || got.LastError != nil {
		t.Fatalf("task after shutdown = %+v, want neutral interruption", got)
	}
	if got.NextRunAt == nil || time.Until(*got.NextRunAt) > time.Second {
		t.Fatalf("interrupted task postponed: %+v", got)
	}
	logs, err := repo.ListEventLogs(context.Background(), 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].EventType != "run_interrupted" || logs[0].Severity != repository.EventLogSeverityInfo {
		t.Fatalf("shutdown audit = %+v, want one neutral interruption", logs)
	}
	rerun := make(chan struct{})
	registry := newTestRegistry(t, Task{Name: "long", IntervalSeconds: 3600, Run: func(context.Context) error { close(rerun); return nil }})
	if err := ReconcileTasks(t.Context(), repo, registry); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(repo, registry, s.log, time.Hour, nil)
	t.Cleanup(restarted.Stop)
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rerun:
	case <-time.After(2 * time.Second):
		t.Fatal("interrupted task did not retry at startup")
	}

}

func TestScheduler_TaskCancellationIsNotShutdown(t *testing.T) {
	s, repo := newTestScheduler(t, Task{Name: "self-cancelled", IntervalSeconds: 3600, Run: func(ctx context.Context) error {
		own, cancel := context.WithCancel(ctx)
		cancel()
		return own.Err()
	}})
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		row, err := repo.GetTask(t.Context(), "self-cancelled")
		return err == nil && row.LastStatus == repository.TaskStatusFailed
	})
	row, _ := repo.GetTask(t.Context(), "self-cancelled")
	if row.LastError == nil || *row.LastError != context.Canceled.Error() {
		t.Fatalf("own cancellation misclassified: %+v", row)
	}
}
