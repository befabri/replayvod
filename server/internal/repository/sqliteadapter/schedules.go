package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateSchedule(ctx context.Context, input *repository.ScheduleInput) (*repository.DownloadSchedule, error) {
	row, err := a.queries.CreateSchedule(ctx, sqlitegen.CreateScheduleParams{
		BroadcasterID:    input.BroadcasterID,
		RequestedBy:      input.RequestedBy,
		Quality:          input.Quality,
		HasMinViewers:    boolToInt64(input.HasMinViewers),
		MinViewers:       int64PtrToNullInt64(input.MinViewers),
		HasCategories:    boolToInt64(input.HasCategories),
		HasTags:          boolToInt64(input.HasTags),
		IsDeleteRediff:   boolToInt64(input.IsDeleteRediff),
		TimeBeforeDelete: int64PtrToNullInt64(input.TimeBeforeDelete),
		IsDisabled:       boolToInt64(input.IsDisabled),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create schedule: %w", err)
	}
	return sqliteScheduleToDomain(row), nil
}

func (a *SQLiteAdapter) GetSchedule(ctx context.Context, id int64) (*repository.DownloadSchedule, error) {
	row, err := a.queries.GetSchedule(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteScheduleToDomain(row), nil
}

func (a *SQLiteAdapter) GetScheduleForUserChannel(ctx context.Context, broadcasterID, userID string) (*repository.DownloadSchedule, error) {
	row, err := a.queries.GetScheduleForUserChannel(ctx, sqlitegen.GetScheduleForUserChannelParams{
		BroadcasterID: broadcasterID,
		RequestedBy:   userID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteScheduleToDomain(row), nil
}

func (a *SQLiteAdapter) UpdateSchedule(ctx context.Context, id int64, input *repository.ScheduleInput) (*repository.DownloadSchedule, error) {
	row, err := a.queries.UpdateSchedule(ctx, sqlitegen.UpdateScheduleParams{
		ID:               id,
		Quality:          input.Quality,
		HasMinViewers:    boolToInt64(input.HasMinViewers),
		MinViewers:       int64PtrToNullInt64(input.MinViewers),
		HasCategories:    boolToInt64(input.HasCategories),
		HasTags:          boolToInt64(input.HasTags),
		IsDeleteRediff:   boolToInt64(input.IsDeleteRediff),
		TimeBeforeDelete: int64PtrToNullInt64(input.TimeBeforeDelete),
		IsDisabled:       boolToInt64(input.IsDisabled),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteScheduleToDomain(row), nil
}

func (a *SQLiteAdapter) ToggleSchedule(ctx context.Context, id int64) (*repository.DownloadSchedule, error) {
	row, err := a.queries.ToggleSchedule(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteScheduleToDomain(row), nil
}

func (a *SQLiteAdapter) DeleteSchedule(ctx context.Context, id int64) error {
	return a.queries.DeleteSchedule(ctx, id)
}

func (a *SQLiteAdapter) ListSchedules(ctx context.Context, limit, offset int) ([]repository.DownloadSchedule, error) {
	rows, err := a.queries.ListSchedules(ctx, sqlitegen.ListSchedulesParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedules: %w", err)
	}
	return sqliteSchedulesToDomain(rows), nil
}

func (a *SQLiteAdapter) ListSchedulesForUser(ctx context.Context, userID string, limit, offset int) ([]repository.DownloadSchedule, error) {
	rows, err := a.queries.ListSchedulesForUser(ctx, sqlitegen.ListSchedulesForUserParams{
		RequestedBy: userID,
		Limit:       int64(limit),
		Offset:      int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedules for user: %w", err)
	}
	return sqliteSchedulesToDomain(rows), nil
}

func (a *SQLiteAdapter) ListActiveSchedulesForBroadcaster(ctx context.Context, broadcasterID string) ([]repository.DownloadSchedule, error) {
	rows, err := a.queries.ListActiveSchedulesForBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list active schedules for broadcaster: %w", err)
	}
	return sqliteSchedulesToDomain(rows), nil
}

func (a *SQLiteAdapter) RecordScheduleTrigger(ctx context.Context, id int64) error {
	return a.queries.RecordScheduleTrigger(ctx, id)
}

func (a *SQLiteAdapter) LinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error {
	return a.queries.LinkScheduleCategory(ctx, sqlitegen.LinkScheduleCategoryParams{
		ScheduleID: scheduleID,
		CategoryID: categoryID,
	})
}

func (a *SQLiteAdapter) UnlinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error {
	return a.queries.UnlinkScheduleCategory(ctx, sqlitegen.UnlinkScheduleCategoryParams{
		ScheduleID: scheduleID,
		CategoryID: categoryID,
	})
}

func (a *SQLiteAdapter) ClearScheduleCategories(ctx context.Context, scheduleID int64) error {
	return a.queries.ClearScheduleCategories(ctx, scheduleID)
}

func (a *SQLiteAdapter) ListScheduleCategories(ctx context.Context, scheduleID int64) ([]repository.Category, error) {
	rows, err := a.queries.ListScheduleCategories(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedule categories: %w", err)
	}
	out := make([]repository.Category, len(rows))
	for i, r := range rows {
		out[i] = *sqliteCategoryToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) LinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error {
	return a.queries.LinkScheduleTag(ctx, sqlitegen.LinkScheduleTagParams{
		ScheduleID: scheduleID,
		TagID:      tagID,
	})
}

func (a *SQLiteAdapter) UnlinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error {
	return a.queries.UnlinkScheduleTag(ctx, sqlitegen.UnlinkScheduleTagParams{
		ScheduleID: scheduleID,
		TagID:      tagID,
	})
}

func (a *SQLiteAdapter) ClearScheduleTags(ctx context.Context, scheduleID int64) error {
	return a.queries.ClearScheduleTags(ctx, scheduleID)
}

func (a *SQLiteAdapter) ListScheduleTags(ctx context.Context, scheduleID int64) ([]repository.Tag, error) {
	rows, err := a.queries.ListScheduleTags(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedule tags: %w", err)
	}
	out := make([]repository.Tag, len(rows))
	for i, r := range rows {
		out[i] = *sqliteTagToDomain(r)
	}
	return out, nil
}

func sqliteScheduleToDomain(s sqlitegen.DownloadSchedule) *repository.DownloadSchedule {
	return &repository.DownloadSchedule{
		ID:               s.ID,
		BroadcasterID:    s.BroadcasterID,
		RequestedBy:      s.RequestedBy,
		Quality:          s.Quality,
		HasMinViewers:    int64ToBool(s.HasMinViewers),
		MinViewers:       nullInt64ToInt64Ptr(s.MinViewers),
		HasCategories:    int64ToBool(s.HasCategories),
		HasTags:          int64ToBool(s.HasTags),
		IsDeleteRediff:   int64ToBool(s.IsDeleteRediff),
		TimeBeforeDelete: nullInt64ToInt64Ptr(s.TimeBeforeDelete),
		IsDisabled:       int64ToBool(s.IsDisabled),
		LastTriggeredAt:  parseNullTime(s.LastTriggeredAt),
		TriggerCount:     s.TriggerCount,
		CreatedAt:        parseTime(s.CreatedAt),
		UpdatedAt:        parseTime(s.UpdatedAt),
	}
}

func sqliteSchedulesToDomain(rows []sqlitegen.DownloadSchedule) []repository.DownloadSchedule {
	out := make([]repository.DownloadSchedule, len(rows))
	for i, r := range rows {
		out[i] = *sqliteScheduleToDomain(r)
	}
	return out
}

// boolToInt64 converts Go's bool to SQLite's INTEGER storage for booleans.
// Kept as a small helper rather than inlined so future "boolean" columns
// (has_*, is_*) share the same convention.
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func int64ToBool(v int64) bool { return v != 0 }

func int64PtrToNullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func nullInt64ToInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
