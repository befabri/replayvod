package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateSchedule(ctx context.Context, input *repository.ScheduleInput) (*repository.DownloadSchedule, error) {
	row, err := a.queries.CreateSchedule(ctx, pggen.CreateScheduleParams{
		BroadcasterID:    input.BroadcasterID,
		RequestedBy:      input.RequestedBy,
		Quality:          input.Quality,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       int64PtrToInt32Ptr(input.MinViewers),
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: int64PtrToInt32Ptr(input.TimeBeforeDelete),
		IsDisabled:       input.IsDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create schedule: %w", err)
	}
	return pgScheduleToDomain(row), nil
}

func (a *PGAdapter) GetSchedule(ctx context.Context, id int64) (*repository.DownloadSchedule, error) {
	row, err := a.queries.GetSchedule(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgScheduleToDomain(row), nil
}

func (a *PGAdapter) GetScheduleForUserChannel(ctx context.Context, broadcasterID, userID string) (*repository.DownloadSchedule, error) {
	row, err := a.queries.GetScheduleForUserChannel(ctx, pggen.GetScheduleForUserChannelParams{
		BroadcasterID: broadcasterID,
		RequestedBy:   userID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgScheduleToDomain(row), nil
}

func (a *PGAdapter) UpdateSchedule(ctx context.Context, id int64, input *repository.ScheduleInput) (*repository.DownloadSchedule, error) {
	row, err := a.queries.UpdateSchedule(ctx, pggen.UpdateScheduleParams{
		ID:               id,
		Quality:          input.Quality,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       int64PtrToInt32Ptr(input.MinViewers),
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: int64PtrToInt32Ptr(input.TimeBeforeDelete),
		IsDisabled:       input.IsDisabled,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgScheduleToDomain(row), nil
}

func (a *PGAdapter) ToggleSchedule(ctx context.Context, id int64) (*repository.DownloadSchedule, error) {
	row, err := a.queries.ToggleSchedule(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgScheduleToDomain(row), nil
}

func (a *PGAdapter) DeleteSchedule(ctx context.Context, id int64) error {
	return a.queries.DeleteSchedule(ctx, id)
}

func (a *PGAdapter) ListSchedules(ctx context.Context, limit, offset int) ([]repository.DownloadSchedule, error) {
	rows, err := a.queries.ListSchedules(ctx, pggen.ListSchedulesParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list schedules: %w", err)
	}
	return pgSchedulesToDomain(rows), nil
}

func (a *PGAdapter) ListSchedulesForUser(ctx context.Context, userID string, limit, offset int) ([]repository.DownloadSchedule, error) {
	rows, err := a.queries.ListSchedulesForUser(ctx, pggen.ListSchedulesForUserParams{
		RequestedBy: userID,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list schedules for user: %w", err)
	}
	return pgSchedulesToDomain(rows), nil
}

func (a *PGAdapter) ListActiveSchedulesForBroadcaster(ctx context.Context, broadcasterID string) ([]repository.DownloadSchedule, error) {
	rows, err := a.queries.ListActiveSchedulesForBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, fmt.Errorf("pg list active schedules for broadcaster: %w", err)
	}
	return pgSchedulesToDomain(rows), nil
}

func (a *PGAdapter) RecordScheduleTrigger(ctx context.Context, id int64) error {
	return a.queries.RecordScheduleTrigger(ctx, id)
}

func (a *PGAdapter) LinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error {
	return a.queries.LinkScheduleCategory(ctx, pggen.LinkScheduleCategoryParams{
		ScheduleID: scheduleID,
		CategoryID: categoryID,
	})
}

func (a *PGAdapter) UnlinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error {
	return a.queries.UnlinkScheduleCategory(ctx, pggen.UnlinkScheduleCategoryParams{
		ScheduleID: scheduleID,
		CategoryID: categoryID,
	})
}

func (a *PGAdapter) ClearScheduleCategories(ctx context.Context, scheduleID int64) error {
	return a.queries.ClearScheduleCategories(ctx, scheduleID)
}

func (a *PGAdapter) ListScheduleCategories(ctx context.Context, scheduleID int64) ([]repository.Category, error) {
	rows, err := a.queries.ListScheduleCategories(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("pg list schedule categories: %w", err)
	}
	out := make([]repository.Category, len(rows))
	for i, r := range rows {
		out[i] = *pgCategoryToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) LinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error {
	return a.queries.LinkScheduleTag(ctx, pggen.LinkScheduleTagParams{
		ScheduleID: scheduleID,
		TagID:      tagID,
	})
}

func (a *PGAdapter) UnlinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error {
	return a.queries.UnlinkScheduleTag(ctx, pggen.UnlinkScheduleTagParams{
		ScheduleID: scheduleID,
		TagID:      tagID,
	})
}

func (a *PGAdapter) ClearScheduleTags(ctx context.Context, scheduleID int64) error {
	return a.queries.ClearScheduleTags(ctx, scheduleID)
}

func (a *PGAdapter) ListScheduleTags(ctx context.Context, scheduleID int64) ([]repository.Tag, error) {
	rows, err := a.queries.ListScheduleTags(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("pg list schedule tags: %w", err)
	}
	out := make([]repository.Tag, len(rows))
	for i, r := range rows {
		out[i] = *pgTagToDomain(r)
	}
	return out, nil
}

func pgScheduleToDomain(s pggen.DownloadSchedule) *repository.DownloadSchedule {
	return &repository.DownloadSchedule{
		ID:               s.ID,
		BroadcasterID:    s.BroadcasterID,
		RequestedBy:      s.RequestedBy,
		Quality:          s.Quality,
		HasMinViewers:    s.HasMinViewers,
		MinViewers:       int32PtrToInt64Ptr(s.MinViewers),
		HasCategories:    s.HasCategories,
		HasTags:          s.HasTags,
		IsDeleteRediff:   s.IsDeleteRediff,
		TimeBeforeDelete: int32PtrToInt64Ptr(s.TimeBeforeDelete),
		IsDisabled:       s.IsDisabled,
		LastTriggeredAt:  s.LastTriggeredAt,
		TriggerCount:     s.TriggerCount,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func pgSchedulesToDomain(rows []pggen.DownloadSchedule) []repository.DownloadSchedule {
	out := make([]repository.DownloadSchedule, len(rows))
	for i, r := range rows {
		out[i] = *pgScheduleToDomain(r)
	}
	return out
}

// int64PtrToInt32Ptr narrows *int64 to *int32 for PG columns declared as
// INTEGER. Overflow would be a caller bug (we don't expect min_viewers or
// time_before_delete to exceed 2B) — the conversion preserves nil-ness.
func int64PtrToInt32Ptr(p *int64) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}

// int32PtrToInt64Ptr is the reverse — domain uses int64 for consistency
// regardless of underlying column width.
func int32PtrToInt64Ptr(p *int32) *int64 {
	if p == nil {
		return nil
	}
	v := int64(*p)
	return &v
}
