package pgadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) ClaimTask(ctx context.Context, name, executionID string) error {
	if executionID == "" {
		return repository.ErrStaleExecution
	}
	n, err := a.queries.ClaimTask(ctx, pggen.ClaimTaskParams{Name: name, ExecutionID: executionID})
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	row, err := a.GetTask(ctx, name)
	if err != nil {
		return err
	}
	if row.ExecutionID == executionID && row.LastStatus == repository.TaskStatusRunning {
		return nil
	}
	return repository.ErrStaleExecution
}

func (a *PGAdapter) SettleTask(ctx context.Context, name, executionID, status string, durationMs int64, message string) error {
	if status != repository.TaskStatusSuccess && status != repository.TaskStatusFailed && status != repository.TaskStatusInterrupted {
		return repository.ErrStaleExecution
	}
	n, err := a.queries.SettleTask(ctx, pggen.SettleTaskParams{Name: name, ExecutionID: executionID, LastStatus: status, LastDurationMs: int32(durationMs), Column5: message})
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	row, err := a.GetTask(ctx, name)
	if err != nil {
		return err
	}
	if row.ExecutionID == executionID && row.LastStatus == status {
		return nil
	}
	return repository.ErrStaleExecution
}
