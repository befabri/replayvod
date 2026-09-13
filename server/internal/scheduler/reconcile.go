package scheduler

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/befabri/replayvod/server/internal/repository"
)

// ReconcileTasks atomically publishes this process's registry to the database.
// Run it on every boot, including with an empty registry, before starting task
// execution. Removed tasks stay in the database as unavailable; upserts change
// only availability and descriptive configuration, preserving operator pauses,
// pending run requests, and completed execution history. Abandoned executions
// become interrupted and eligible for retry, without unpausing a task.
// One scheduler owns availability for the database; this is not a reconciliation
// of multiple processes' tasks.
func ReconcileTasks(ctx context.Context, repo repository.Repository, registry Registry) error {
	err := repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.RecoverInterruptedTasks(ctx); err != nil {
			return fmt.Errorf("recover interrupted executions: %w", err)
		}
		if err := tx.ResetTaskAvailability(ctx); err != nil {
			return fmt.Errorf("reset availability: %w", err)
		}
		// A stable order also gives concurrent transactions the same lock order.
		for _, name := range slices.Sorted(maps.Keys(registry.tasks)) {
			task := registry.tasks[name]
			if _, err := tx.UpsertTask(ctx, task.Name, task.Description, task.IntervalSeconds); err != nil {
				return fmt.Errorf("upsert task %q: %w", task.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scheduler: reconcile tasks: %w", err)
	}
	return nil
}
