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
	if _, err := a.queries.SetTaskNextRun(ctx, name); err != nil {
		return mapErr(err)
	}
	return nil
}

func (a *PGAdapter) ScheduleTaskIfEnabled(ctx context.Context, name string) error {
	_, err := a.queries.ScheduleTaskIfEnabled(ctx, name)
	return mapErr(err)
}
