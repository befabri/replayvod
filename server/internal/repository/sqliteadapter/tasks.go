package sqliteadapter

import (
	"context"
	"database/sql"
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

func (a *SQLiteAdapter) GetTask(ctx context.Context, name string) (*repository.Task, error) {
	row, err := a.queries.GetTask(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteTaskToDomain(row), nil
}

func (a *SQLiteAdapter) ListTasks(ctx context.Context) ([]repository.Task, error) {
	rows, err := a.queries.ListTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list tasks: %w", err)
	}
	return sqliteTasksToDomain(rows), nil
}

func (a *SQLiteAdapter) ListDueTasks(ctx context.Context) ([]repository.Task, error) {
	rows, err := a.queries.ListDueTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list due tasks: %w", err)
	}
	return sqliteTasksToDomain(rows), nil
}

func (a *SQLiteAdapter) MarkTaskRunning(ctx context.Context, name string) error {
	return a.queries.MarkTaskRunning(ctx, name)
}

func (a *SQLiteAdapter) MarkTaskSuccess(ctx context.Context, name string, durationMs int64) error {
	return a.queries.MarkTaskSuccess(ctx, sqlitegen.MarkTaskSuccessParams{
		Name:           name,
		LastDurationMs: durationMs,
	})
}

func (a *SQLiteAdapter) MarkTaskFailed(ctx context.Context, name string, durationMs int64, errMsg string) error {
	return a.queries.MarkTaskFailed(ctx, sqlitegen.MarkTaskFailedParams{
		Name:           name,
		LastDurationMs: durationMs,
		LastError:      sql.NullString{String: errMsg, Valid: true},
	})
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
	return a.queries.SetTaskNextRun(ctx, name)
}

func sqliteTaskToDomain(t sqlitegen.Task) *repository.Task {
	return &repository.Task{
		Name:            t.Name,
		Description:     t.Description,
		IntervalSeconds: int32(t.IntervalSeconds),
		IsEnabled:       int64ToBool(t.IsEnabled),
		LastRunAt:       parseNullTime(t.LastRunAt),
		LastDurationMs:  int32(t.LastDurationMs),
		LastStatus:      t.LastStatus,
		LastError:       fromNullString(t.LastError),
		NextRunAt:       parseNullTime(t.NextRunAt),
		CreatedAt:       parseTime(t.CreatedAt),
		UpdatedAt:       parseTime(t.UpdatedAt),
	}
}

func sqliteTasksToDomain(rows []sqlitegen.Task) []repository.Task {
	out := make([]repository.Task, len(rows))
	for i, r := range rows {
		out[i] = *sqliteTaskToDomain(r)
	}
	return out
}
