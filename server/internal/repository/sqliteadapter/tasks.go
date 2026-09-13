package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) UpsertTask(ctx context.Context, name, description string, intervalSeconds int64) (*repository.Task, error) {
	row, err := a.queries.UpsertTask(ctx, sqlitegen.UpsertTaskParams{
		Name:            name,
		Description:     description,
		IntervalSeconds: intervalSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert task: %w", err)
	}
	return sqliteTaskToDomain(row), nil
}

func (a *SQLiteAdapter) SetTaskEnabled(ctx context.Context, name string, enabled bool) (*repository.Task, error) {
	row, err := a.queries.SetTaskEnabled(ctx, sqlitegen.SetTaskEnabledParams{
		Name:      name,
		IsEnabled: boolToInt64(enabled),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteTaskToDomain(row), nil
}

func (a *SQLiteAdapter) SetTaskNextRun(ctx context.Context, name string) error {
	if _, err := a.queries.SetTaskNextRun(ctx, name); err != nil {
		return mapErr(err)
	}
	return nil
}

func (a *SQLiteAdapter) ScheduleTaskIfEnabled(ctx context.Context, name string) error {
	_, err := a.queries.ScheduleTaskIfEnabled(ctx, name)
	return mapErr(err)
}
