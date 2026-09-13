package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
)

type taskFaults struct {
	repository.Repository
	claimBlocked  atomic.Bool
	settleBlocked atomic.Bool
	claimCalls    atomic.Int32
	settleCalls   atomic.Int32
}

func (r *taskFaults) ClaimTask(ctx context.Context, name, executionID string) error {
	r.claimCalls.Add(1)
	if r.claimBlocked.Load() {
		return errors.New("claim database unavailable")
	}
	return r.Repository.ClaimTask(ctx, name, executionID)
}

func (r *taskFaults) SettleTask(ctx context.Context, name, executionID, status string, durationMs int64, message string) error {
	r.settleCalls.Add(1)
	if r.settleBlocked.Load() {
		return errors.New("settlement database unavailable")
	}
	return r.Repository.SettleTask(ctx, name, executionID, status, durationMs, message)
}

func TestTaskSettlementPreservesScheduling(t *testing.T) {
	var runs atomic.Int32
	s, repo := newTestScheduler(t, Task{Name: "durable", IntervalSeconds: 86400, Run: func(context.Context) error { runs.Add(1); return nil }})
	faults := &taskFaults{Repository: repo}
	faults.claimBlocked.Store(true)
	faults.settleBlocked.Store(true)
	s.repo = faults
	s.bus = eventbus.New()
	changes := s.bus.TaskStatus.Subscribe(t.Context())
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return faults.claimCalls.Load() > 0 })
	for range 3 {
		s.tick()
	}
	if count := s.runner.Used("task"); count != 1 {
		t.Fatalf("repeated discovery duplicated unresolved task ownership: %d", count)
	}
	if runs.Load() != 0 {
		t.Fatal("effects started before claim confirmation")
	}
	faults.claimBlocked.Store(false)
	waitFor(t, 2*time.Second, func() bool { return faults.settleCalls.Load() > 0 })
	if runs.Load() != 1 {
		t.Fatalf("runs=%d", runs.Load())
	}
	if err := repo.SetTaskNextRun(t.Context(), "durable"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetTaskEnabled(t.Context(), "durable", false); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-changes:
			if event.Status != repository.TaskStatusRunning {
				t.Fatalf("uncommitted outcome published: %+v", event)
			}
		default:
			goto recovered
		}
	}
recovered:
	faults.settleBlocked.Store(false)
	waitFor(t, 2*time.Second, func() bool {
		row, err := repo.GetTask(t.Context(), "durable")
		return err == nil && row.LastStatus == repository.TaskStatusSuccess
	})
	row, err := repo.GetTask(t.Context(), "durable")
	if err != nil || row.IsEnabled || row.NextRunAt == nil || time.Until(*row.NextRunAt) > time.Second {
		t.Fatalf("settlement lost scheduling: %+v %v", row, err)
	}
	if runs.Load() != 1 {
		t.Fatal("persistence retry repeated task effects")
	}
}

type uncertainTaskWrites struct {
	repository.Repository
	lostClaim, lostSettlement atomic.Bool
}

func (r *uncertainTaskWrites) ClaimTask(ctx context.Context, name, executionID string) error {
	err := r.Repository.ClaimTask(ctx, name, executionID)
	if err == nil && !r.lostClaim.Swap(true) {
		return repository.ErrCommitUncertain
	}
	return err
}

func (r *uncertainTaskWrites) SettleTask(ctx context.Context, name, executionID, status string, durationMs int64, message string) error {
	err := r.Repository.SettleTask(ctx, name, executionID, status, durationMs, message)
	if err == nil && !r.lostSettlement.Swap(true) {
		return repository.ErrCommitUncertain
	}
	return err
}

func TestTaskLostConfirmationsDoNotRepeatEffectsOrLosePanic(t *testing.T) {
	var runs atomic.Int32
	s, repo := newTestScheduler(t, Task{Name: "panic-task", IntervalSeconds: 86400, Run: func(context.Context) error {
		runs.Add(1)
		panic("task failure")
	}})
	faults := &uncertainTaskWrites{Repository: repo}
	s.repo = faults
	s.bus = eventbus.New()
	changes := s.bus.TaskStatus.Subscribe(t.Context())
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		return faults.lostClaim.Load() && faults.lostSettlement.Load() && s.runner.Used("task") == 0
	})
	row, err := repo.GetTask(t.Context(), "panic-task")
	if err != nil || row.LastStatus != repository.TaskStatusFailed || row.LastError == nil || *row.LastError != "worker panic: task failure" || row.NextRunAt == nil {
		t.Fatalf("panic settlement = %+v, %v", row, err)
	}
	if runs.Load() != 1 {
		t.Fatalf("lost confirmation repeated task effects: %d", runs.Load())
	}
	for _, want := range []string{repository.TaskStatusRunning, repository.TaskStatusFailed} {
		select {
		case ev := <-changes:
			if ev.Status != want {
				t.Fatalf("published status=%s, want %s", ev.Status, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing confirmed %s notification", want)
		}
	}
}
