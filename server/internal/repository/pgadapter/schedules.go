package pgadapter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateSchedule(ctx context.Context, input *repository.ScheduleInput) (*repository.DownloadSchedule, error) {
	row, err := a.queries.CreateSchedule(ctx, pgCreateScheduleParams(input))
	if err != nil {
		return nil, fmt.Errorf("pg create schedule: %w", err)
	}
	return pgDownloadScheduleToDomain(row), nil
}

func (a *PGAdapter) CreateScheduleWithFilters(ctx context.Context, input *repository.ScheduleInput, filters repository.ScheduleFilterInput) (*repository.DownloadSchedule, error) {
	var out *repository.DownloadSchedule
	err := a.inTx(ctx, func(q *pggen.Queries, _ pgx.Tx) error {
		row, err := q.CreateSchedule(ctx, pgCreateScheduleParams(input))
		if err != nil {
			return fmt.Errorf("pg create schedule: %w", mapErr(err))
		}
		sched := pgDownloadScheduleToDomain(row)
		if err := replacePGScheduleFilters(ctx, q, sched.ID, filters); err != nil {
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

func (a *PGAdapter) UpdateSchedule(ctx context.Context, id int64, input *repository.ScheduleInput) (*repository.DownloadSchedule, error) {
	row, err := a.queries.UpdateSchedule(ctx, pgUpdateScheduleParams(id, input))
	if err != nil {
		return nil, mapErr(err)
	}
	return pgDownloadScheduleToDomain(row), nil
}

func (a *PGAdapter) UpdateScheduleWithFilters(ctx context.Context, id int64, input *repository.ScheduleInput, filters repository.ScheduleFilterInput) (*repository.DownloadSchedule, error) {
	var out *repository.DownloadSchedule
	err := a.inTx(ctx, func(q *pggen.Queries, _ pgx.Tx) error {
		row, err := q.UpdateSchedule(ctx, pgUpdateScheduleParams(id, input))
		if err != nil {
			return fmt.Errorf("pg update schedule: %w", mapErr(err))
		}
		sched := pgDownloadScheduleToDomain(row)
		if err := replacePGScheduleFilters(ctx, q, sched.ID, filters); err != nil {
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

func pgCreateScheduleParams(input *repository.ScheduleInput) pggen.CreateScheduleParams {
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: input.RecordingType,
		Quality:       input.Quality,
		ForceH264:     input.ForceH264,
	})
	return pggen.CreateScheduleParams{
		BroadcasterID:    input.BroadcasterID,
		RequestedBy:      input.RequestedBy,
		RequestedFrom:    input.RequestedFrom,
		RecordingType:    settings.RecordingType,
		Quality:          settings.Quality,
		ForceH264:        settings.ForceH264,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       int64PtrToInt32Ptr(input.MinViewers),
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: int64PtrToInt32Ptr(input.TimeBeforeDelete),
		IsDisabled:       input.IsDisabled,
	}
}

func pgUpdateScheduleParams(id int64, input *repository.ScheduleInput) pggen.UpdateScheduleParams {
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: input.RecordingType,
		Quality:       input.Quality,
		ForceH264:     input.ForceH264,
	})
	return pggen.UpdateScheduleParams{
		ID:               id,
		RecordingType:    settings.RecordingType,
		Quality:          settings.Quality,
		ForceH264:        settings.ForceH264,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       int64PtrToInt32Ptr(input.MinViewers),
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: int64PtrToInt32Ptr(input.TimeBeforeDelete),
		IsDisabled:       input.IsDisabled,
	}
}

func replacePGScheduleFilters(ctx context.Context, q *pggen.Queries, scheduleID int64, filters repository.ScheduleFilterInput) error {
	if err := q.ClearScheduleCategories(ctx, scheduleID); err != nil {
		return fmt.Errorf("pg clear schedule categories %d: %w", scheduleID, err)
	}
	for _, id := range filters.CategoryIDs {
		if err := q.LinkScheduleCategory(ctx, pggen.LinkScheduleCategoryParams{ScheduleID: scheduleID, CategoryID: id}); err != nil {
			return fmt.Errorf("pg link schedule category %s to schedule %d: %w", id, scheduleID, err)
		}
	}
	if err := q.ClearScheduleTags(ctx, scheduleID); err != nil {
		return fmt.Errorf("pg clear schedule tags %d: %w", scheduleID, err)
	}
	for _, id := range filters.TagIDs {
		if err := q.LinkScheduleTag(ctx, pggen.LinkScheduleTagParams{ScheduleID: scheduleID, TagID: id}); err != nil {
			return fmt.Errorf("pg link schedule tag %d to schedule %d: %w", id, scheduleID, err)
		}
	}
	return nil
}

func pgDownloadScheduleToDomain(s pggen.DownloadSchedule) *repository.DownloadSchedule {
	return &repository.DownloadSchedule{
		ID:               s.ID,
		BroadcasterID:    s.BroadcasterID,
		RequestedBy:      s.RequestedBy,
		RequestedFrom:    s.RequestedFrom,
		RecordingType:    repository.NormalizeRecordingType(s.RecordingType),
		Quality:          s.Quality,
		ForceH264:        s.ForceH264,
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

func pgDownloadSchedulesToDomain(rows []pggen.DownloadSchedule) []repository.DownloadSchedule {
	out := make([]repository.DownloadSchedule, len(rows))
	for i, r := range rows {
		out[i] = *pgDownloadScheduleToDomain(r)
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
