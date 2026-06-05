package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateSchedule(ctx context.Context, input *repository.ScheduleInput) (*repository.DownloadSchedule, error) {
	row, err := a.queries.CreateSchedule(ctx, sqliteCreateScheduleParams(input))
	if err != nil {
		return nil, fmt.Errorf("sqlite create schedule: %w", err)
	}
	return sqliteScheduleToDomain(row), nil
}

func (a *SQLiteAdapter) CreateScheduleWithFilters(ctx context.Context, input *repository.ScheduleInput, filters repository.ScheduleFilterInput) (*repository.DownloadSchedule, error) {
	var out *repository.DownloadSchedule
	err := a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		row, err := q.CreateSchedule(ctx, sqliteCreateScheduleParams(input))
		if err != nil {
			return fmt.Errorf("sqlite create schedule: %w", err)
		}
		sched := sqliteScheduleToDomain(row)
		if err := replaceSQLiteScheduleFilters(ctx, q, sched.ID, filters); err != nil {
			return err
		}
		out = sched
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
	row, err := a.queries.UpdateSchedule(ctx, sqliteUpdateScheduleParams(id, input))
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteScheduleToDomain(row), nil
}

func (a *SQLiteAdapter) UpdateScheduleWithFilters(ctx context.Context, id int64, input *repository.ScheduleInput, filters repository.ScheduleFilterInput) (*repository.DownloadSchedule, error) {
	var out *repository.DownloadSchedule
	err := a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		row, err := q.UpdateSchedule(ctx, sqliteUpdateScheduleParams(id, input))
		if err != nil {
			return fmt.Errorf("sqlite update schedule: %w", mapErr(err))
		}
		sched := sqliteScheduleToDomain(row)
		if err := replaceSQLiteScheduleFilters(ctx, q, sched.ID, filters); err != nil {
			return err
		}
		out = sched
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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

func sqliteCreateScheduleParams(input *repository.ScheduleInput) sqlitegen.CreateScheduleParams {
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: input.RecordingType,
		Quality:       input.Quality,
		ForceH264:     input.ForceH264,
	})
	return sqlitegen.CreateScheduleParams{
		BroadcasterID:    input.BroadcasterID,
		RequestedBy:      input.RequestedBy,
		RecordingType:    settings.RecordingType,
		Quality:          settings.Quality,
		ForceH264:        boolToInt64(settings.ForceH264),
		HasMinViewers:    boolToInt64(input.HasMinViewers),
		MinViewers:       int64PtrToNullInt64(input.MinViewers),
		HasCategories:    boolToInt64(input.HasCategories),
		HasTags:          boolToInt64(input.HasTags),
		IsDeleteRediff:   boolToInt64(input.IsDeleteRediff),
		TimeBeforeDelete: int64PtrToNullInt64(input.TimeBeforeDelete),
		IsDisabled:       boolToInt64(input.IsDisabled),
	}
}

func sqliteUpdateScheduleParams(id int64, input *repository.ScheduleInput) sqlitegen.UpdateScheduleParams {
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: input.RecordingType,
		Quality:       input.Quality,
		ForceH264:     input.ForceH264,
	})
	return sqlitegen.UpdateScheduleParams{
		ID:               id,
		RecordingType:    settings.RecordingType,
		Quality:          settings.Quality,
		ForceH264:        boolToInt64(settings.ForceH264),
		HasMinViewers:    boolToInt64(input.HasMinViewers),
		MinViewers:       int64PtrToNullInt64(input.MinViewers),
		HasCategories:    boolToInt64(input.HasCategories),
		HasTags:          boolToInt64(input.HasTags),
		IsDeleteRediff:   boolToInt64(input.IsDeleteRediff),
		TimeBeforeDelete: int64PtrToNullInt64(input.TimeBeforeDelete),
		IsDisabled:       boolToInt64(input.IsDisabled),
	}
}

func replaceSQLiteScheduleFilters(ctx context.Context, q *sqlitegen.Queries, scheduleID int64, filters repository.ScheduleFilterInput) error {
	if err := q.ClearScheduleCategories(ctx, scheduleID); err != nil {
		return fmt.Errorf("sqlite clear schedule categories %d: %w", scheduleID, err)
	}
	for _, id := range filters.CategoryIDs {
		if err := q.LinkScheduleCategory(ctx, sqlitegen.LinkScheduleCategoryParams{ScheduleID: scheduleID, CategoryID: id}); err != nil {
			return fmt.Errorf("sqlite link schedule category %s to schedule %d: %w", id, scheduleID, err)
		}
	}
	if err := q.ClearScheduleTags(ctx, scheduleID); err != nil {
		return fmt.Errorf("sqlite clear schedule tags %d: %w", scheduleID, err)
	}
	for _, id := range filters.TagIDs {
		if err := q.LinkScheduleTag(ctx, sqlitegen.LinkScheduleTagParams{ScheduleID: scheduleID, TagID: id}); err != nil {
			return fmt.Errorf("sqlite link schedule tag %d to schedule %d: %w", id, scheduleID, err)
		}
	}
	return nil
}

func sqliteScheduleToDomain(s sqlitegen.DownloadSchedule) *repository.DownloadSchedule {
	return &repository.DownloadSchedule{
		ID:               s.ID,
		BroadcasterID:    s.BroadcasterID,
		RequestedBy:      s.RequestedBy,
		RecordingType:    repository.NormalizeRecordingType(s.RecordingType),
		Quality:          s.Quality,
		ForceH264:        int64ToBool(s.ForceH264),
		HasMinViewers:    int64ToBool(s.HasMinViewers),
		MinViewers:       nullInt64ToInt64Ptr(s.MinViewers),
		HasCategories:    int64ToBool(s.HasCategories),
		HasTags:          int64ToBool(s.HasTags),
		IsDeleteRediff:   int64ToBool(s.IsDeleteRediff),
		TimeBeforeDelete: nullInt64ToInt64Ptr(s.TimeBeforeDelete),
		IsDisabled:       int64ToBool(s.IsDisabled),
		LastTriggeredAt:  timePtrFromSQLite(s.LastTriggeredAt),
		TriggerCount:     s.TriggerCount,
		CreatedAt:        s.CreatedAt.Time,
		UpdatedAt:        s.UpdatedAt.Time,
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
