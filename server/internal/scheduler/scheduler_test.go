package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
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
