package scheduler

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestNewRegistryValidation(t *testing.T) {
	run := func(context.Context) error { return nil }
	valid := Task{Name: "valid", IntervalSeconds: 60, Run: run}
	for _, tc := range []struct {
		name string
		task Task
		want string
	}{
		{"missing name", Task{Run: run}, "name required"},
		{"missing handler", Task{Name: "nil"}, "no Run func"},
		{"duplicate", valid, "duplicate task"},
		{"negative interval", Task{Name: "negative", IntervalSeconds: -1, Run: run}, "interval must be"},
		{"overflow", Task{Name: "overflow", IntervalSeconds: math.MaxInt32 + 1, Run: run}, "interval must be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, err := NewRegistry([]Task{valid, tc.task})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewRegistry error = %v, want %q", err, tc.want)
			}
			if registry.Len() != 0 {
				t.Fatal("invalid construction returned a partial registry")
			}
		})
	}
	for _, seconds := range []int64{0, math.MaxInt32} {
		valid.IntervalSeconds = seconds
		if registry, err := NewRegistry([]Task{valid}); err != nil || registry.Len() != 1 {
			t.Fatalf("interval %d: registry=%+v, error=%v", seconds, registry, err)
		}
	}
}

func TestRegistryOwnsDefinitionsForReconciliationAndExecution(t *testing.T) {
	repo := newTestRepo(t)
	var originalRuns, replacementRuns atomic.Int32
	tasks := []Task{{Name: "original", Description: "original description", IntervalSeconds: 3600,
		Run: func(context.Context) error { originalRuns.Add(1); return nil },
	}}
	registry := newTestRegistry(t, tasks...)
	svc := NewService(repo, registry, slog.New(slog.DiscardHandler), time.Hour, nil)
	t.Cleanup(svc.Stop)
	// Reusing the caller's slice must not change either reconciliation or the
	// handler admitted by the scheduler, even with the same task name.
	tasks[0].Description = "replacement description"
	tasks[0].IntervalSeconds = 0
	tasks[0].Run = func(context.Context) error { replacementRuns.Add(1); return nil }
	if err := ReconcileTasks(t.Context(), repo, registry); err != nil {
		t.Fatal(err)
	}
	row, err := repo.GetTask(t.Context(), "original")
	if err != nil || row.Description != "original description" || row.IntervalSeconds != 3600 {
		t.Fatalf("caller changed registry metadata: %+v, %v", row, err)
	}
	tasks[0].Name = "replacement"
	if err := svc.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		row, err := repo.GetTask(t.Context(), "original")
		return err == nil && row.LastStatus == repository.TaskStatusSuccess
	})
	if originalRuns.Load() != 1 || replacementRuns.Load() != 0 {
		t.Fatalf("caller changed registry execution: original=%d replacement=%d", originalRuns.Load(), replacementRuns.Load())
	}
}
