package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) LinkStreamCategory(ctx context.Context, streamID, categoryID string) error {
	return a.queries.LinkStreamCategory(ctx, sqlitegen.LinkStreamCategoryParams{StreamID: streamID, CategoryID: categoryID})
}

func (a *SQLiteAdapter) LinkVideoCategory(ctx context.Context, videoID int64, categoryID string) error {
	return a.queries.LinkVideoCategory(ctx, sqlitegen.LinkVideoCategoryParams{VideoID: videoID, CategoryID: categoryID})
}

// UpsertVideoCategorySpan runs the close-previous-span + insert-
// new-span pair in a tx. See UpsertVideoTitleSpan for rationale.
func (a *SQLiteAdapter) UpsertVideoCategorySpan(ctx context.Context, videoID int64, categoryID string, at time.Time) error {
	ts := formatTime(at.UTC())
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := q.CloseOtherOpenVideoCategorySpans(ctx, sqlitegen.CloseOtherOpenVideoCategorySpansParams{
			AtTime:     sql.NullString{String: ts, Valid: true},
			VideoID:    videoID,
			CategoryID: categoryID,
		}); err != nil {
			return fmt.Errorf("sqlite close other open video category spans: %w", err)
		}
		if err := q.InsertVideoCategorySpan(ctx, sqlitegen.InsertVideoCategorySpanParams{
			VideoID:    videoID,
			CategoryID: categoryID,
			AtTime:     ts,
		}); err != nil {
			return fmt.Errorf("sqlite insert video category span: %w", err)
		}
		return nil
	})
}

func (a *SQLiteAdapter) LinkStreamTag(ctx context.Context, streamID string, tagID int64) error {
	return a.queries.LinkStreamTag(ctx, sqlitegen.LinkStreamTagParams{StreamID: streamID, TagID: tagID})
}

func (a *SQLiteAdapter) LinkVideoTag(ctx context.Context, videoID, tagID int64) error {
	return a.queries.LinkVideoTag(ctx, sqlitegen.LinkVideoTagParams{VideoID: videoID, TagID: tagID})
}

func (a *SQLiteAdapter) ListCategoriesForVideo(ctx context.Context, videoID int64) ([]repository.CategorySpan, error) {
	rows, err := a.queries.ListCategorySpansForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list category spans for video: %w", err)
	}
	out := make([]repository.CategorySpan, len(rows))
	for i, r := range rows {
		span := repository.CategorySpan{
			Category: repository.Category{
				ID:        r.ID,
				Name:      r.Name,
				BoxArtURL: fromNullString(r.BoxArtUrl),
				IGDBID:    fromNullString(r.IgdbID),
				CreatedAt: parseTime(r.CreatedAt),
				UpdatedAt: parseTime(r.UpdatedAt),
			},
			StartedAt:       parseTime(r.StartedAt),
			DurationSeconds: anyToFloat64(r.DurationSeconds),
		}
		if r.EndedAt.Valid {
			v := parseTime(r.EndedAt.String)
			span.EndedAt = &v
		}
		out[i] = span
	}
	return out, nil
}

// ListPrimaryCategoriesForVideos takes the first (highest-ranked) row
// per video_id. The sqlc query's ORDER BY guarantees: highest total
// duration, then earliest first-seen, then name. SQLite lacks
// DISTINCT ON so the dedup is done here rather than in SQL.
func (a *SQLiteAdapter) ListPrimaryCategoriesForVideos(ctx context.Context, videoIDs []int64) (map[int64]repository.Category, error) {
	if len(videoIDs) == 0 {
		return map[int64]repository.Category{}, nil
	}
	rows, err := a.queries.ListPrimaryCategoriesForVideos(ctx, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("sqlite list primary categories for videos: %w", err)
	}
	out := make(map[int64]repository.Category, len(videoIDs))
	for _, r := range rows {
		if _, exists := out[r.VideoID]; exists {
			continue
		}
		out[r.VideoID] = repository.Category{
			ID:        r.ID,
			Name:      r.Name,
			BoxArtURL: fromNullString(r.BoxArtUrl),
			IGDBID:    fromNullString(r.IgdbID),
			CreatedAt: parseTime(r.CreatedAt),
			UpdatedAt: parseTime(r.UpdatedAt),
		}
	}
	return out, nil
}

func (a *SQLiteAdapter) ListTagsForVideo(ctx context.Context, videoID int64) ([]repository.Tag, error) {
	rows, err := a.queries.ListTagsForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list tags for video: %w", err)
	}
	out := make([]repository.Tag, len(rows))
	for i, r := range rows {
		out[i] = *sqliteTagToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) AddVideoRequest(ctx context.Context, videoID int64, userID string) error {
	return a.queries.AddVideoRequest(ctx, sqlitegen.AddVideoRequestParams{VideoID: videoID, UserID: userID})
}

func (a *SQLiteAdapter) ListVideoRequestsForUser(ctx context.Context, userID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideoRequestsForUser(ctx, sqlitegen.ListVideoRequestsForUserParams{
		UserID: userID,
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list video requests: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}
