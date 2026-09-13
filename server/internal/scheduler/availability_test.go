package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestTaskPauseAndHistorySurviveConfigurationAcrossBoots(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	const name = "recordings_retention"
	if _, err := repo.UpsertTask(ctx, name, "retention", 60); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, name, "previous"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, name, "previous", repository.TaskStatusSuccess, 42, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetTaskEnabled(ctx, name, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetTaskNextRun(ctx, name); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetTask(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	var effects atomic.Int32
	for _, configured := range []bool{true, false, true} {
		var tasks []Task
		if configured {
			tasks = append(tasks, Task{Name: name, Description: "retention", IntervalSeconds: 60, Run: func(context.Context) error { effects.Add(1); return nil }})
		}
		registry := newTestRegistry(t, tasks...)
		if err := ReconcileTasks(ctx, repo, registry); err != nil {
			t.Fatal(err)
		}
		s := NewService(repo, registry, slog.New(slog.DiscardHandler), time.Hour, nil)
		t.Cleanup(s.Stop)
		if err := s.Start(ctx); err != nil {
			t.Fatal(err)
		}
		s.tick()
		s.Stop()
		row, err := repo.GetTask(ctx, name)
		if err != nil || row.IsEnabled || row.IsAvailable != configured {
			t.Fatalf("config=%v lost pause/availability: %+v, %v", configured, row, err)
		}
		if row.LastStatus != before.LastStatus || row.LastDurationMs != 42 || row.ExecutionID != "previous" || row.LastRunAt == nil || !row.LastRunAt.Equal(*before.LastRunAt) || row.NextRunAt == nil || !row.NextRunAt.Equal(*before.NextRunAt) {
			t.Fatalf("config=%v lost task history: %+v", configured, row)
		}
	}
	if effects.Load() != 0 {
		t.Fatal("configuration changes resumed operator-paused retention")
	}
}

type disableBeforeClaim struct {
	repository.Repository
	claims atomic.Int32
}

func (r *disableBeforeClaim) ClaimTask(ctx context.Context, name, execution string) error {
	r.claims.Add(1)
	if _, err := r.Repository.SetTaskEnabled(ctx, name, false); err != nil {
		return err
	}
	return r.Repository.ClaimTask(ctx, name, execution)
}

func TestTaskDisabledAfterDiscoveryReleasesRejectedClaim(t *testing.T) {
	var effects atomic.Int32
	s, repo := newTestScheduler(t, Task{Name: "paused", IntervalSeconds: 60, Run: func(context.Context) error { effects.Add(1); return nil }})
	fault := &disableBeforeClaim{Repository: repo}
	s.repo = fault
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return fault.claims.Load() > 0 && s.runner.Used("task") == 0 })
	if effects.Load() != 0 || fault.claims.Load() != 1 {
		t.Fatalf("rejected task repeated: effects=%d claims=%d", effects.Load(), fault.claims.Load())
	}
	if err := repo.ClaimTask(t.Context(), "paused", "late"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("paused claim = %v", err)
	}
}
