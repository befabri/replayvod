package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

type pollRecorder struct {
	repository.Repository
	polls atomic.Int32
}

func (r *pollRecorder) ListDueTasks(ctx context.Context) ([]repository.Task, error) {
	r.polls.Add(1)
	if r.Repository == nil {
		return nil, nil
	}
	return r.Repository.ListDueTasks(ctx)
}

func TestSchedulerStartOnlyLaunchesExecution(t *testing.T) {
	for _, populated := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var registry Registry
			if populated {
				registry = newTestRegistry(t, Task{Name: "configured", IntervalSeconds: 60, Run: func(context.Context) error { return nil }})
			}
			// Only polling is implemented. Any reconciliation in NewService or
			// Start would panic through the nil embedded repository.
			repo := &pollRecorder{}
			s := NewService(repo, registry, slog.New(slog.DiscardHandler), time.Second, nil)
			t.Cleanup(s.Stop)
			if err := s.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			if populated && repo.polls.Load() != 1 {
				t.Fatal("nonempty registry did not poll immediately")
			}
			time.Sleep(3 * time.Second)
			synctest.Wait()
			if !populated && repo.polls.Load() != 0 {
				t.Fatal("empty registry launched a poller")
			}
			if populated && repo.polls.Load() < 3 {
				t.Fatal("nonempty registry stopped polling when no tasks were due")
			}
			if err := s.Start(t.Context()); err == nil {
				t.Fatal("second Start succeeded")
			}
			s.Stop()
			polls := repo.polls.Load()
			time.Sleep(3 * time.Second)
			synctest.Wait()
			if repo.polls.Load() != polls {
				t.Fatal("polling continued after Stop")
			}
			if err := s.Start(t.Context()); err == nil {
				t.Fatal("Start after Stop succeeded")
			}
		})
	}
}

func TestSchedulerStopBeforeStartAndCancelledStart(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	stopped := NewService(nil, Registry{}, log, time.Hour, nil)
	stopped.Stop()
	stopped.Stop()
	if err := stopped.Start(t.Context()); err == nil {
		t.Fatal("Start after Stop succeeded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := NewService(nil, Registry{}, log, time.Hour, nil)
	t.Cleanup(s.Stop)
	if err := s.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Start = %v", err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("cancelled startup prevented retry: %v", err)
	}
}

func TestSchedulerPauseResumeAndQueuedRunRemainLive(t *testing.T) {
	var runs atomic.Int32
	s, repo := newTestScheduler(t, Task{Name: "pausable", IntervalSeconds: 3600, Run: func(context.Context) error { runs.Add(1); return nil }})
	polls := &pollRecorder{Repository: repo}
	s.repo = polls
	if _, err := repo.SetTaskEnabled(t.Context(), "pausable", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, wantRuns := range []int32{1, 2} {
		if err := repo.SetTaskNextRun(t.Context(), "pausable"); err != nil {
			t.Fatal(err)
		}
		before := polls.polls.Load()
		waitFor(t, time.Second, func() bool { return polls.polls.Load() >= before+2 })
		if runs.Load() != wantRuns-1 {
			t.Fatal("paused task executed a queued run")
		}
		if _, err := repo.SetTaskEnabled(t.Context(), "pausable", true); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 2*time.Second, func() bool {
			row, err := repo.GetTask(t.Context(), "pausable")
			return err == nil && row.LastStatus == repository.TaskStatusSuccess && runs.Load() == wantRuns && s.runner.Used("task") == 0
		})
		if _, err := repo.SetTaskEnabled(t.Context(), "pausable", false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSchedulerZeroIntervalRequiresExplicitRun(t *testing.T) {
	var runs atomic.Int32
	s, repo := newTestScheduler(t, Task{Name: "manual", Run: func(context.Context) error { runs.Add(1); return nil }})
	polls := &pollRecorder{Repository: repo}
	s.repo = polls
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return polls.polls.Load() >= 2 })
	if runs.Load() != 0 {
		t.Fatal("manual task ran without a request")
	}
	if err := repo.SetTaskNextRun(t.Context(), "manual"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		row, err := repo.GetTask(t.Context(), "manual")
		return err == nil && row.LastStatus == repository.TaskStatusSuccess && s.runner.Used("task") == 0
	})
	before := polls.polls.Load()
	waitFor(t, time.Second, func() bool { return polls.polls.Load() >= before+2 })
	row, err := repo.GetTask(t.Context(), "manual")
	if err != nil || runs.Load() != 1 || row.NextRunAt != nil {
		t.Fatalf("manual task was rearmed: runs=%d, row=%+v, error=%v", runs.Load(), row, err)
	}
}

func TestSchedulerRestartExecutesAbandonedOneShotOnce(t *testing.T) {
	repo := newTestRepo(t)
	var runs atomic.Int32
	registry := newTestRegistry(t, Task{Name: "one-shot", Run: func(context.Context) error {
		runs.Add(1)
		return nil
	}})
	if err := ReconcileTasks(t.Context(), repo, registry); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetTaskNextRun(t.Context(), "one-shot"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(t.Context(), "one-shot", "abandoned-execution"); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after claim consumed the manual request, then run the
	// same startup reconciliation and polling sequence used by the server.
	if err := ReconcileTasks(t.Context(), repo, registry); err != nil {
		t.Fatal(err)
	}
	polls := &pollRecorder{Repository: repo}
	s := NewService(polls, registry, slog.New(slog.DiscardHandler), 20*time.Millisecond, nil)
	t.Cleanup(s.Stop)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		row, err := repo.GetTask(t.Context(), "one-shot")
		return err == nil && row.LastStatus == repository.TaskStatusSuccess && s.runner.Used("task") == 0
	})
	before := polls.polls.Load()
	waitFor(t, time.Second, func() bool { return polls.polls.Load() >= before+2 })
	row, err := repo.GetTask(t.Context(), "one-shot")
	if err != nil || row.ExecutionID == "abandoned-execution" || row.NextRunAt != nil || runs.Load() != 1 {
		t.Fatalf("recovered task did not settle exactly once: runs=%d row=%+v error=%v", runs.Load(), row, err)
	}
}

func TestSchedulerParentCancellationInterruptsJobs(t *testing.T) {
	started := make(chan struct{})
	s, repo := newTestScheduler(t, Task{Name: "long", IntervalSeconds: 3600, Run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not start")
	}
	cancel()
	// Observe settlement before calling Stop, proving that the parent context
	// alone stops execution. Stop remains the operation that drains shutdown.
	waitFor(t, 2*time.Second, func() bool {
		row, err := repo.GetTask(t.Context(), "long")
		return err == nil && row.LastStatus == repository.TaskStatusInterrupted && s.runner.Used("task") == 0
	})
	s.Stop()
}

type shutdownRepo struct {
	repository.Repository
	settling chan struct{}
	release  chan struct{}
}

func (r *shutdownRepo) ListDueTasks(context.Context) ([]repository.Task, error) {
	return []repository.Task{{Name: "long"}}, nil
}

func (r *shutdownRepo) ClaimTask(context.Context, string, string) error { return nil }

func (r *shutdownRepo) SettleTask(context.Context, string, string, string, int64, string) error {
	close(r.settling)
	<-r.release
	return nil
}

func (r *shutdownRepo) CreateEventLog(context.Context, *repository.EventLogInput) (*repository.EventLog, error) {
	return &repository.EventLog{}, nil
}

func TestSchedulerConcurrentStopsWaitForSettlement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &shutdownRepo{settling: make(chan struct{}), release: make(chan struct{})}
		registry := newTestRegistry(t, Task{Name: "long", IntervalSeconds: 60, Run: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}})
		s := NewService(repo, registry, slog.New(slog.DiscardHandler), time.Hour, nil)
		defer s.Stop()
		defer close(repo.release)
		if err := s.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if s.runner.Used("task") != 1 {
			t.Fatal("task was not admitted")
		}
		returned := make(chan struct{}, 2)
		for range 2 {
			go func() { s.Stop(); returned <- struct{}{} }()
		}
		synctest.Wait()
		select {
		case <-repo.settling:
		default:
			t.Fatal("Stop did not cancel the running task")
		}
		if len(returned) != 0 {
			t.Fatal("Stop returned before task settlement finished")
		}
		// Release settlement without closing the channel, so the deferred close
		// can also unblock cleanup if an assertion above fails.
		repo.release <- struct{}{}
		synctest.Wait()
		if len(returned) != 2 {
			t.Fatal("concurrent Stop callers did not both finish")
		}
	})
}

func TestSchedulerConcurrentStartAndStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &pollRecorder{}
		registry := newTestRegistry(t, Task{Name: "configured", Run: func(context.Context) error { return nil }})
		s := NewService(repo, registry, slog.New(slog.DiscardHandler), time.Second, nil)
		t.Cleanup(s.Stop)
		started := make(chan error, 1)
		stopped := make(chan struct{})
		go func() { started <- s.Start(t.Context()) }()
		go func() { s.Stop(); close(stopped) }()
		synctest.Wait()
		<-started // Either operation may win; neither may strand a goroutine.
		<-stopped
		before := repo.polls.Load()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if repo.polls.Load() != before {
			t.Fatal("concurrent Start left a poller after Stop returned")
		}
	})
}
