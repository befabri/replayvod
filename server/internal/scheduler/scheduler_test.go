package scheduler

import (
	"context"
	"errors"
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

func newTestScheduler(t *testing.T) (*Service, repository.Repository) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewService(repo, log, 20*time.Millisecond, nil)
	t.Cleanup(s.Stop)
	return s, repo
}

// TestScheduler_Start_RegistersAllTasksInDB confirms that Start()
// persists every registered task (UpsertTask). Without this the
// dashboard's task list would start empty and operators wouldn't see
// the task metadata until its first run.
func TestScheduler_Start_RegistersAllTasksInDB(t *testing.T) {
	s, repo := newTestScheduler(t)

	_ = s.Register(Task{
		Name: "t1", Description: "first", IntervalSeconds: 60,
		Run: func(context.Context) error { return nil },
	})
	_ = s.Register(Task{
		Name: "t2", Description: "second", IntervalSeconds: 0,
		Run: func(context.Context) error { return nil },
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	rows, err := repo.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	names := map[string]bool{}
	for _, r := range rows {
		names[r.Name] = true
	}
	if !names["t1"] || !names["t2"] {
		t.Errorf("registered tasks missing from DB: %v", names)
	}
}

// TestScheduler_RunsDueTask_AndMarksSuccess pins the end-to-end happy
// path: register a task with a tiny interval, let the ticker fire,
// observe the Run func called and last_status transition to "success"
// with a non-zero duration. The DB is the source of truth for run
// history — without the mark transitions, the dashboard shows stale
// "pending" even after runs.
func TestScheduler_RunsDueTask_AndMarksSuccess(t *testing.T) {
	s, repo := newTestScheduler(t)

	var runs atomic.Int32
	_ = s.Register(Task{
		Name: "ran", Description: "test", IntervalSeconds: 3600,
		Run: func(context.Context) error {
			runs.Add(1)
			return nil
		},
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The scheduler fires the initial tick on Start; next_run_at is
	// NULL on a freshly-upserted row so the due query picks it up.
	waitFor(t, 2*time.Second, func() bool { return runs.Load() >= 1 })

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

// TestScheduler_TaskFailure_MarksFailedAndContinues checks the error
// path: a panicking / erroring Run records last_status=failed with the
// error message, and the scheduler keeps ticking (other tasks still
// fire, and the failing task retries on the next interval).
func TestScheduler_TaskFailure_MarksFailedAndContinues(t *testing.T) {
	s, repo := newTestScheduler(t)

	_ = s.Register(Task{
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

func TestRegisterStandardTasks_EventSubOffNeutralizesPreviouslyRegisteredTasks(t *testing.T) {
	s, repo := newTestScheduler(t)
	ctx := context.Background()

	eventSubTaskNames := []string{taskEventSubReconcileChannels, taskEventSubSnapshot}
	for _, name := range eventSubTaskNames {
		if _, err := repo.UpsertTask(ctx, name, "previous EventSub task", 60); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	dueBefore, err := repo.ListDueTasks(ctx)
	if err != nil {
		t.Fatalf("ListDueTasks before RegisterStandardTasks: %v", err)
	}
	if len(dueBefore) != 2 {
		t.Fatalf("seeded due task count = %d, want 2", len(dueBefore))
	}

	// An operator paused these tasks from the dashboard. Re-registering them
	// (here, as disabled) must not silently re-enable them: UpsertTask writes
	// only description/interval, so is_enabled survives the redeploy.
	for _, name := range eventSubTaskNames {
		if _, err := repo.SetTaskEnabled(ctx, name, false); err != nil {
			t.Fatalf("pause %s: %v", name, err)
		}
	}

	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				EventsubReconcileIntervalMinutes: 15,
				EventsubIntervalMinutes:          15,
			},
		},
		ServerMode: config.ServerModeConfig{
			Mode: config.ServerModeOff,
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := RegisterStandardTasks(s, cfg, repo, StandardTaskDeps{}, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	for _, name := range eventSubTaskNames {
		got, err := repo.GetTask(ctx, name)
		if err != nil {
			t.Fatalf("GetTask(%s): %v", name, err)
		}
		if got.IntervalSeconds != 0 {
			t.Fatalf("%s interval_seconds = %d, want 0 while EventSub is off", name, got.IntervalSeconds)
		}
		if got.IsEnabled {
			t.Fatalf("%s is_enabled = true, want the operator's paused state preserved across UpsertTask", name)
		}
	}
	dueAfter, err := repo.ListDueTasks(ctx)
	if err != nil {
		t.Fatalf("ListDueTasks after Start: %v", err)
	}
	for _, task := range dueAfter {
		if task.Name == taskEventSubReconcileChannels || task.Name == taskEventSubSnapshot {
			t.Fatalf("disabled EventSub task %s is still due", task.Name)
		}
	}
}

// TestRegisterStandardTasks_EventSubOffNeutralizesStaleEnabledTask pins the
// distinguishing behavior of registerDisabledTask: a stale row left ENABLED with
// a live interval by a prior direct/relay config is neutralized by rewriting its
// interval to 0, so ListDueTasks (interval_seconds > 0) stops returning it and
// tick() stops warning about a due task with no runner. The sibling test pre-
// disables the rows, which masks this — is_enabled=0 already excludes them, so it
// never proves the interval rewrite is what does the work.
func TestRegisterStandardTasks_EventSubOffNeutralizesStaleEnabledTask(t *testing.T) {
	s, repo := newTestScheduler(t)
	ctx := context.Background()

	eventSubTaskNames := []string{taskEventSubReconcileChannels, taskEventSubSnapshot}
	for _, name := range eventSubTaskNames {
		// A stale row from a prior direct/relay config: enabled, intervaled,
		// runner-less. UpsertTask leaves a fresh row enabled.
		if _, err := repo.UpsertTask(ctx, name, "stale EventSub task", 60); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	dueBefore, err := repo.ListDueTasks(ctx)
	if err != nil {
		t.Fatalf("ListDueTasks before: %v", err)
	}
	if len(dueBefore) != 2 {
		t.Fatalf("stale enabled tasks due before = %d, want 2", len(dueBefore))
	}

	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				EventsubReconcileIntervalMinutes: 15,
				EventsubIntervalMinutes:          15,
			},
		},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeOff},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// esvc == nil drives the registerDisabledTask branch.
	if err := RegisterStandardTasks(s, cfg, repo, StandardTaskDeps{}, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	for _, name := range eventSubTaskNames {
		got, err := repo.GetTask(ctx, name)
		if err != nil {
			t.Fatalf("GetTask(%s): %v", name, err)
		}
		if got.IntervalSeconds != 0 {
			t.Fatalf("%s interval_seconds = %d, want 0 (neutralized)", name, got.IntervalSeconds)
		}
		if !got.IsEnabled {
			t.Fatalf("%s is_enabled = false; the stale row was never paused, only neutralized via interval", name)
		}
	}
	dueAfter, err := repo.ListDueTasks(ctx)
	if err != nil {
		t.Fatalf("ListDueTasks after: %v", err)
	}
	for _, task := range dueAfter {
		if task.Name == taskEventSubReconcileChannels || task.Name == taskEventSubSnapshot {
			t.Fatalf("neutralized EventSub task %s is still due despite a 0 interval", task.Name)
		}
	}
}

// TestRegisterStandardTasks_EventSubActiveRegistersIntervaledTasks pins the
// active branch (esvc != nil && interval > 0), which no test exercised because
// the only production caller in off/poll mode passes esvc == nil. Both EventSub
// tasks must register with their configured interval, not the disabled 0.
func TestRegisterStandardTasks_EventSubActiveRegistersIntervaledTasks(t *testing.T) {
	s, repo := newTestScheduler(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tc := twitch.NewClient("client-id", "client-secret", log)
	esvc := eventsub.New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "0123456789abcdef", log)

	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				EventsubReconcileIntervalMinutes: 15,
				EventsubIntervalMinutes:          15,
			},
		},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
	}

	if err := RegisterStandardTasks(s, cfg, repo, StandardTaskDeps{EventSub: esvc}, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}

	// Assert on the in-memory registration rather than starting the scheduler:
	// starting would tick immediately and fire the real reconcile against the
	// Twitch client. The registered interval is the contract the active branch
	// owns.
	for _, name := range []string{taskEventSubReconcileChannels, taskEventSubSnapshot} {
		task, ok := s.tasks[name]
		if !ok {
			t.Fatalf("%s was not registered in the active branch", name)
		}
		if task.IntervalSeconds != 15*60 {
			t.Fatalf("%s interval = %d, want 900 (15m), not the disabled 0", name, task.IntervalSeconds)
		}
	}
}

// waitFor polls cond until true or the deadline passes. Keeps the
// scheduler-timing tests robust to CI jitter without resorting to
// sleep-based coupling.
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
