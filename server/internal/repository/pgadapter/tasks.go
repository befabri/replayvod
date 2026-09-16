package pgadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) SetTaskEnabled(ctx context.Context, name string, enabled bool) (*repository.Task, error) {
	row, err := a.queries.SetTaskEnabled(ctx, pggen.SetTaskEnabledParams{
		Name:    name,
		Enabled: enabled,
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
