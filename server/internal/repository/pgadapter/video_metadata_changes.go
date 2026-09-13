package pgadapter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

// RecordVideoMetadataChange implements repository.Repository's atomic observation
// write; callers must fetch category artwork after commit to avoid network waits
// while holding the recording lock.
func (a *PGAdapter) RecordVideoMetadataChange(
	ctx context.Context,
	input repository.VideoMetadataChangeInput,
) (*repository.VideoMetadataChangeResult, error) {
	if input.Title == "" && input.CategoryID == "" {
		return nil, repository.ErrNoMetadataObserved
	}

	result := &repository.VideoMetadataChangeResult{}
	at := input.OccurredAt.UTC()

	err := a.inTx(ctx, func(q *pggen.Queries, _ pgx.Tx) error {
		v, err := q.GetVideoForUpdate(ctx, input.VideoID)
		if err != nil {
			return mapErr(err)
		}
		j, err := q.GetJob(ctx, input.JobID)
		if err != nil {
			return mapErr(err)
		}
		if !repository.MetadataEligible(pgVideoToDomain(v), pgJobToDomain(j), input) {
			return repository.ErrStaleExecution
		}

		var titleID *int64
		if input.Title != "" {
			t, err := q.UpsertTitle(ctx, input.Title)
			if err != nil {
				return fmt.Errorf("pg upsert title: %w", err)
			}
			if err := q.LinkVideoTitle(ctx, pggen.LinkVideoTitleParams{
				VideoID: input.VideoID,
				TitleID: t.ID,
			}); err != nil {
				return fmt.Errorf("pg link video title: %w", err)
			}
			if err := q.UpsertVideoTitleSpan(ctx, pggen.UpsertVideoTitleSpanParams{
				VideoID: input.VideoID,
				TitleID: t.ID,
				AtTime:  at,
			}); err != nil {
				return fmt.Errorf("pg upsert video title span: %w", err)
			}
			titleID = &t.ID
			result.Title = pgTitleToDomain(t)
		}

		var categoryID *string
		if input.CategoryID != "" {
			if input.CategoryName != "" {
				c, err := q.UpsertCategory(ctx, pggen.UpsertCategoryParams{
					ID:   input.CategoryID,
					Name: input.CategoryName,
				})
				if err != nil {
					return fmt.Errorf("pg upsert category: %w", err)
				}
				result.Category = pgCategoryToDomain(c)
			}
			if err := q.LinkVideoCategory(ctx, pggen.LinkVideoCategoryParams{
				VideoID:    input.VideoID,
				CategoryID: input.CategoryID,
			}); err != nil {
				return fmt.Errorf("pg link video category: %w", err)
			}
			if err := q.UpsertVideoCategorySpan(ctx, pggen.UpsertVideoCategorySpanParams{
				VideoID:    input.VideoID,
				CategoryID: input.CategoryID,
				AtTime:     at,
			}); err != nil {
				return fmt.Errorf("pg upsert video category span: %w", err)
			}
			id := input.CategoryID
			categoryID = &id
			// An omitted category name must preserve the stored name for artwork lookup.
			if result.Category == nil {
				cat, err := q.GetCategory(ctx, input.CategoryID)
				if err == nil {
					result.Category = pgCategoryToDomain(cat)
				}
				// Category lookup failure only skips optional artwork enrichment.
			}
		}

		if _, err := q.InsertVideoMetadataChange(ctx, pggen.InsertVideoMetadataChangeParams{
			VideoID:            input.VideoID,
			OccurredAt:         at,
			TitleID:            titleID,
			CategoryID:         categoryID,
			MediaOffsetSeconds: input.MediaOffsetSeconds,
		}); err != nil {
			return fmt.Errorf("pg insert video metadata change: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (a *PGAdapter) ListVideoMetadataChanges(
	ctx context.Context,
	videoID int64,
) ([]repository.VideoMetadataChange, error) {
	rows, err := a.queries.ListVideoMetadataChangesForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list video metadata changes: %w", err)
	}
	out := make([]repository.VideoMetadataChange, len(rows))
	for i, r := range rows {
		event := repository.VideoMetadataChange{
			ID:                 r.ID,
			VideoID:            r.VideoID,
			OccurredAt:         r.OccurredAt,
			MediaOffsetSeconds: r.MediaOffsetSeconds,
		}
		// LEFT JOIN columns are nullable even when their source columns are NOT NULL.
		if r.TitleID != nil && r.TitleName != nil {
			t := repository.Title{
				ID:   *r.TitleID,
				Name: *r.TitleName,
			}
			if r.TitleCreatedAt != nil {
				t.CreatedAt = *r.TitleCreatedAt
			}
			event.Title = &t
		}
		if r.CategoryID != nil && r.CategoryName != nil {
			c := repository.Category{
				ID:        *r.CategoryID,
				Name:      *r.CategoryName,
				BoxArtURL: r.CategoryBoxArtUrl,
				IGDBID:    r.CategoryIgdbID,
			}
			if r.CategoryCreatedAt != nil {
				c.CreatedAt = *r.CategoryCreatedAt
			}
			if r.CategoryUpdatedAt != nil {
				c.UpdatedAt = *r.CategoryUpdatedAt
			}
			event.Category = &c
		}
		out[i] = event
	}
	return out, nil
}
