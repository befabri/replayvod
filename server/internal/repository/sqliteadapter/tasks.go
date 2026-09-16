package sqliteadapter

import (
	"context"
)

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
