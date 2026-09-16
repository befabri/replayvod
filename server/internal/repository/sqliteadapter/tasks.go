package sqliteadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) SetTaskEnabled(ctx context.Context, name string, enabled bool) (*repository.Task, error) {
	row, err := a.queries.SetTaskEnabled(ctx, sqlitegen.SetTaskEnabledParams{
		Name:    name,
		Enabled: boolToInt64(enabled),
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
