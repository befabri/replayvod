package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// RecordVideoMetadataChange implements repository.Repository's atomic observation
// write; callers must fetch category artwork after commit to avoid network waits
// while holding the recording lock.
func (a *SQLiteAdapter) RecordVideoMetadataChange(
	ctx context.Context,
	input repository.VideoMetadataChangeInput,
) (*repository.VideoMetadataChangeResult, error) {
	if input.Title == "" && input.CategoryID == "" {
		return nil, repository.ErrNoMetadataObserved
	}

	result := &repository.VideoMetadataChangeResult{}
	at := input.OccurredAt.UTC()
	ts := sqliteTime(at)

	err := a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		v, err := q.GetVideoForUpdate(ctx, input.VideoID)
		if err != nil {
			return mapErr(err)
		}
		j, err := q.GetJob(ctx, input.JobID)
		if err != nil {
			return mapErr(err)
		}
		if !repository.MetadataEligible(sqliteVideoToDomain(v), sqliteJobToDomain(j), input) {
			return repository.ErrStaleExecution
		}

		var titleID sql.NullInt64
		if input.Title != "" {
			t, err := q.UpsertTitle(ctx, input.Title)
			if err != nil {
				return fmt.Errorf("sqlite upsert title: %w", err)
			}
			if err := q.LinkVideoTitle(ctx, sqlitegen.LinkVideoTitleParams{
				VideoID: input.VideoID,
				TitleID: t.ID,
			}); err != nil {
				return fmt.Errorf("sqlite link video title: %w", err)
			}
			// Close and insert spans in the observation transaction so readers cannot see
			// a partially applied title change.
			if err := q.CloseOtherOpenVideoTitleSpans(ctx, sqlitegen.CloseOtherOpenVideoTitleSpansParams{
				At:      &ts,
				VideoID: input.VideoID,
				TitleID: t.ID,
			}); err != nil {
				return fmt.Errorf("sqlite close other open video title spans: %w", err)
			}
			if err := q.InsertVideoTitleSpan(ctx, sqlitegen.InsertVideoTitleSpanParams{
				VideoID: input.VideoID,
				TitleID: t.ID,
				At:      ts,
			}); err != nil {
				return fmt.Errorf("sqlite insert video title span: %w", err)
			}
			titleID = sql.NullInt64{Int64: t.ID, Valid: true}
			result.Title = sqliteTitleToDomain(t)
		}

		var categoryID sql.NullString
		if input.CategoryID != "" {
			if input.CategoryName != "" {
				c, err := q.UpsertCategory(ctx, sqlitegen.UpsertCategoryParams{
					ID:   input.CategoryID,
					Name: input.CategoryName,
				})
				if err != nil {
					return fmt.Errorf("sqlite upsert category: %w", err)
				}
				result.Category = sqliteCategoryToDomain(c)
			}
			if err := q.LinkVideoCategory(ctx, sqlitegen.LinkVideoCategoryParams{
				VideoID:    input.VideoID,
				CategoryID: input.CategoryID,
			}); err != nil {
				return fmt.Errorf("sqlite link video category: %w", err)
			}
			if err := q.CloseOtherOpenVideoCategorySpans(ctx, sqlitegen.CloseOtherOpenVideoCategorySpansParams{
				At:         &ts,
				VideoID:    input.VideoID,
				CategoryID: input.CategoryID,
			}); err != nil {
				return fmt.Errorf("sqlite close other open video category spans: %w", err)
			}
			if err := q.InsertVideoCategorySpan(ctx, sqlitegen.InsertVideoCategorySpanParams{
				VideoID:    input.VideoID,
				CategoryID: input.CategoryID,
				At:         ts,
			}); err != nil {
				return fmt.Errorf("sqlite insert video category span: %w", err)
			}
			categoryID = sql.NullString{String: input.CategoryID, Valid: true}
			// An omitted category name must preserve the stored name for artwork lookup.
			if result.Category == nil {
				cat, err := q.GetCategory(ctx, input.CategoryID)
				if err == nil {
					result.Category = sqliteCategoryToDomain(cat)
				}
			}
		}

		if _, err := q.InsertVideoMetadataChange(ctx, sqlitegen.InsertVideoMetadataChangeParams{
			VideoID:            input.VideoID,
			OccurredAt:         ts,
			TitleID:            titleID,
			CategoryID:         categoryID,
			MediaOffsetSeconds: nullFloat64(input.MediaOffsetSeconds),
		}); err != nil {
			return fmt.Errorf("sqlite insert video metadata change: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (a *SQLiteAdapter) ListVideoMetadataChanges(
	ctx context.Context,
	videoID int64,
) ([]repository.VideoMetadataChange, error) {
	rows, err := a.queries.ListVideoMetadataChangesForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list video metadata changes: %w", err)
	}
	out := make([]repository.VideoMetadataChange, len(rows))
	for i, r := range rows {
		event := repository.VideoMetadataChange{
			ID:                 r.ID,
			VideoID:            r.VideoID,
			OccurredAt:         r.OccurredAt.Time,
			MediaOffsetSeconds: fromNullFloat64(r.MediaOffsetSeconds),
		}
		if r.TitleID.Valid && r.TitleName.Valid {
			t := repository.Title{
				ID:   r.TitleID.Int64,
				Name: r.TitleName.String,
			}
			t.CreatedAt = r.TitleCreatedAt.Time
			event.Title = &t
		}
		if r.CategoryID.Valid && r.CategoryName.Valid {
			c := repository.Category{
				ID:        r.CategoryID.String,
				Name:      r.CategoryName.String,
				BoxArtURL: fromNullString(r.CategoryBoxArtUrl),
				IGDBID:    fromNullString(r.CategoryIgdbID),
			}
			c.CreatedAt = r.CategoryCreatedAt.Time
			c.UpdatedAt = r.CategoryUpdatedAt.Time
			event.Category = &c
		}
		out[i] = event
	}
	return out, nil
}
