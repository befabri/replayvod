package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) UpsertTask(ctx context.Context, name, description string, intervalSeconds int64) (*repository.Task, error) {
	row, err := a.queries.UpsertTask(ctx, pggen.UpsertTaskParams{
		Name:            name,
		Description:     description,
		IntervalSeconds: int32(intervalSeconds),
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert task: %w", err)
	}
	return pgTaskToDomain(row), nil
}

func (a *PGAdapter) GetTask(ctx context.Context, name string) (*repository.Task, error) {
	row, err := a.queries.GetTask(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgTaskToDomain(row), nil
}

func (a *PGAdapter) ListTasks(ctx context.Context) ([]repository.Task, error) {
	rows, err := a.queries.ListTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list tasks: %w", err)
	}
	return pgTasksToDomain(rows), nil
}

func (a *PGAdapter) ListDueTasks(ctx context.Context) ([]repository.Task, error) {
	rows, err := a.queries.ListDueTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list due tasks: %w", err)
	}
	return pgTasksToDomain(rows), nil
}

func (a *PGAdapter) MarkTaskRunning(ctx context.Context, name string) error {
	return a.queries.MarkTaskRunning(ctx, name)
}

func (a *PGAdapter) MarkTaskSuccess(ctx context.Context, name string, durationMs int64) error {
	return a.queries.MarkTaskSuccess(ctx, pggen.MarkTaskSuccessParams{
		Name:           name,
		LastDurationMs: int32(durationMs),
	})
}

func (a *PGAdapter) MarkTaskFailed(ctx context.Context, name string, durationMs int64, errMsg string) error {
	e := errMsg
	return a.queries.MarkTaskFailed(ctx, pggen.MarkTaskFailedParams{
		Name:           name,
		LastDurationMs: int32(durationMs),
		LastError:      &e,
	})
}

func (a *PGAdapter) SetTaskEnabled(ctx context.Context, name string, enabled bool) (*repository.Task, error) {
	row, err := a.queries.SetTaskEnabled(ctx, pggen.SetTaskEnabledParams{
		Name:      name,
		IsEnabled: enabled,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgTaskToDomain(row), nil
}

func (a *PGAdapter) SetTaskNextRun(ctx context.Context, name string) error {
	return a.queries.SetTaskNextRun(ctx, name)
}

func pgTaskToDomain(t pggen.Task) *repository.Task {
	return &repository.Task{
		Name:            t.Name,
		Description:     t.Description,
		IntervalSeconds: t.IntervalSeconds,
		IsEnabled:       t.IsEnabled,
		LastRunAt:       t.LastRunAt,
		LastDurationMs:  t.LastDurationMs,
		LastStatus:      t.LastStatus,
		LastError:       t.LastError,
		NextRunAt:       t.NextRunAt,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}

func pgTasksToDomain(rows []pggen.Task) []repository.Task {
	out := make([]repository.Task, len(rows))
	for i, r := range rows {
		out[i] = *pgTaskToDomain(r)
	}
	return out
}
