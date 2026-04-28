package pgadapter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

// RecordVideoMetadataChange runs the title + category + event-row
// writes for a single observed channel.update inside one transaction.
// Either the whole event lands or none of it does — no half-formed
// timeline rows. Empty title and category short-circuit before
// opening the tx.
//
// Art enrich is intentionally NOT wrapped in this tx — it's a
// best-effort Helix call that can be slow or fail; running it inside
// a tx would hold a row lock across a network round trip. The caller
// receives the upserted Category in the result and drives enrich
// itself after commit.
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
			// When CategoryName was empty we didn't UpsertCategory,
			// so result.Category is still nil. Hydrate from the
			// existing row so the caller can decide on enrich.
			if result.Category == nil {
				cat, err := q.GetCategory(ctx, input.CategoryID)
				if err == nil {
					result.Category = pgCategoryToDomain(cat)
				}
				// A miss here is benign: enrich won't fire, the
				// link still landed, and the row exists by FK.
			}
		}

		if _, err := q.InsertVideoMetadataChange(ctx, pggen.InsertVideoMetadataChangeParams{
			VideoID:    input.VideoID,
			OccurredAt: at,
			TitleID:    titleID,
			CategoryID: categoryID,
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
			ID:         r.ID,
			VideoID:    r.VideoID,
			OccurredAt: r.OccurredAt,
		}
		// LEFT JOIN: every non-id column on the joined side is
		// nullable. We hydrate Title/Category only when both the
		// id and name landed — id alone with no name means the FK
		// row was deleted (ON DELETE RESTRICT prevents this in
		// practice, but defensive).
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
