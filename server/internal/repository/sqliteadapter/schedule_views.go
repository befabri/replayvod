package sqliteadapter

import (
	"context"
	"fmt"
	"github.com/befabri/replayvod/server/internal/repository"
)

func (a *SQLiteAdapter) ListUserDisplayNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.queries.ListUserDisplayNames(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list user display names: %w", err)
	}
	for _, row := range rows {
		out[row.ID] = row.DisplayName
	}
	return out, nil
}

func (a *SQLiteAdapter) ListScheduleCategoriesByScheduleIDs(ctx context.Context, ids []int64) (map[int64][]repository.Category, error) {
	out := make(map[int64][]repository.Category, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.queries.ListScheduleCategoriesByScheduleIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedule categories: %w", err)
	}
	for _, row := range rows {
		out[row.ScheduleID] = append(out[row.ScheduleID], *sqliteCategoryToDomain(row.Category))
	}
	return out, nil
}

func (a *SQLiteAdapter) ListScheduleTagsByScheduleIDs(ctx context.Context, ids []int64) (map[int64][]repository.Tag, error) {
	out := make(map[int64][]repository.Tag, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.queries.ListScheduleTagsByScheduleIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedule tags: %w", err)
	}
	for _, row := range rows {
		out[row.ScheduleID] = append(out[row.ScheduleID], *sqliteTagToDomain(row.Tag))
	}
	return out, nil
}
