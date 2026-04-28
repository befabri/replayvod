package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// RecordVideoMetadataChange runs the title + category + event-row
// writes for a single observed channel.update inside one transaction.
// Either the whole event lands or none of it does. Empty title and
// category short-circuit before opening the tx.
//
// Art enrich is intentionally NOT wrapped — see the pgadapter copy
// for rationale. The caller drives enrich after commit using the
// returned Category.
func (a *SQLiteAdapter) RecordVideoMetadataChange(
	ctx context.Context,
	input repository.VideoMetadataChangeInput,
) (*repository.VideoMetadataChangeResult, error) {
	if input.Title == "" && input.CategoryID == "" {
		return nil, repository.ErrNoMetadataObserved
	}

	result := &repository.VideoMetadataChangeResult{}
	at := input.OccurredAt.UTC()
	ts := formatTime(at)

	err := a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
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
			// Span pair: close any previously-open span on a
			// different title, then insert the new one. Mirrors
			// UpsertVideoTitleSpan in titles.go which uses its own
			// inTx — here we share the outer tx instead.
			if err := q.CloseOtherOpenVideoTitleSpans(ctx, sqlitegen.CloseOtherOpenVideoTitleSpansParams{
				AtTime:  sql.NullString{String: ts, Valid: true},
				VideoID: input.VideoID,
				TitleID: t.ID,
			}); err != nil {
				return fmt.Errorf("sqlite close other open video title spans: %w", err)
			}
			if err := q.InsertVideoTitleSpan(ctx, sqlitegen.InsertVideoTitleSpanParams{
				VideoID: input.VideoID,
				TitleID: t.ID,
				AtTime:  ts,
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
				AtTime:     sql.NullString{String: ts, Valid: true},
				VideoID:    input.VideoID,
				CategoryID: input.CategoryID,
			}); err != nil {
				return fmt.Errorf("sqlite close other open video category spans: %w", err)
			}
			if err := q.InsertVideoCategorySpan(ctx, sqlitegen.InsertVideoCategorySpanParams{
				VideoID:    input.VideoID,
				CategoryID: input.CategoryID,
				AtTime:     ts,
			}); err != nil {
				return fmt.Errorf("sqlite insert video category span: %w", err)
			}
			categoryID = sql.NullString{String: input.CategoryID, Valid: true}
			// Hydrate the existing category for the caller's
			// enrich check when CategoryName was empty (a
			// webhook/poll path that didn't carry a fresh name).
			if result.Category == nil {
				cat, err := q.GetCategory(ctx, input.CategoryID)
				if err == nil {
					result.Category = sqliteCategoryToDomain(cat)
				}
			}
		}

		if _, err := q.InsertVideoMetadataChange(ctx, sqlitegen.InsertVideoMetadataChangeParams{
			VideoID:    input.VideoID,
			OccurredAt: ts,
			TitleID:    titleID,
			CategoryID: categoryID,
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
			ID:         r.ID,
			VideoID:    r.VideoID,
			OccurredAt: parseTime(r.OccurredAt),
		}
		if r.TitleID.Valid && r.TitleName.Valid {
			t := repository.Title{
				ID:   r.TitleID.Int64,
				Name: r.TitleName.String,
			}
			if r.TitleCreatedAt.Valid {
				t.CreatedAt = parseTime(r.TitleCreatedAt.String)
			}
			event.Title = &t
		}
		if r.CategoryID.Valid && r.CategoryName.Valid {
			c := repository.Category{
				ID:        r.CategoryID.String,
				Name:      r.CategoryName.String,
				BoxArtURL: fromNullString(r.CategoryBoxArtUrl),
				IGDBID:    fromNullString(r.CategoryIgdbID),
			}
			if r.CategoryCreatedAt.Valid {
				c.CreatedAt = parseTime(r.CategoryCreatedAt.String)
			}
			if r.CategoryUpdatedAt.Valid {
				c.UpdatedAt = parseTime(r.CategoryUpdatedAt.String)
			}
			event.Category = &c
		}
		out[i] = event
	}
	return out, nil
}
