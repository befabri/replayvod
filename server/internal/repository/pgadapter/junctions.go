package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

const upsertVideoCategorySpanSQL = `WITH close_previous AS (
    UPDATE video_category_spans vcs
       SET ended_at = $3,
           duration_seconds = vcs.duration_seconds + EXTRACT(EPOCH FROM ($3 - vcs.started_at))
     WHERE vcs.video_id = $1
       AND vcs.ended_at IS NULL
       AND vcs.category_id <> $2
)
INSERT INTO video_category_spans (video_id, category_id, started_at)
VALUES ($1, $2, $3)
ON CONFLICT (video_id, category_id) WHERE ended_at IS NULL DO NOTHING`

const listCategoriesForVideoSQL = `SELECT
    c.id,
    c.name,
    c.box_art_url,
    c.igdb_id,
    c.created_at,
    c.updated_at,
    vcs.started_at,
    vcs.ended_at,
    vcs.duration_seconds + CASE
        WHEN vcs.ended_at IS NULL THEN EXTRACT(EPOCH FROM (NOW() - vcs.started_at))
        ELSE 0
    END AS duration_seconds
FROM categories c
INNER JOIN video_category_spans vcs ON vcs.category_id = c.id
WHERE vcs.video_id = $1
ORDER BY vcs.started_at ASC, vcs.id ASC`

const listPrimaryCategoriesForVideosSQL = `SELECT DISTINCT ON (agg.video_id)
    agg.video_id,
    c.id,
    c.name,
    c.box_art_url,
    c.igdb_id,
    c.created_at,
    c.updated_at,
    agg.duration_seconds
FROM (
    SELECT
        vcs.video_id,
        vcs.category_id,
        SUM(vcs.duration_seconds + CASE
            WHEN vcs.ended_at IS NULL THEN EXTRACT(EPOCH FROM (NOW() - vcs.started_at))
            ELSE 0
        END) AS duration_seconds,
        MIN(vcs.started_at) AS first_seen_at
    FROM video_category_spans vcs
    WHERE vcs.video_id = ANY($1)
    GROUP BY vcs.video_id, vcs.category_id
) agg
INNER JOIN categories c ON c.id = agg.category_id
ORDER BY
    agg.video_id,
    duration_seconds DESC,
    agg.first_seen_at ASC,
    c.name ASC`

func (a *PGAdapter) LinkStreamCategory(ctx context.Context, streamID, categoryID string) error {
	return a.queries.LinkStreamCategory(ctx, pggen.LinkStreamCategoryParams{StreamID: streamID, CategoryID: categoryID})
}

func (a *PGAdapter) LinkVideoCategory(ctx context.Context, videoID int64, categoryID string) error {
	return a.queries.LinkVideoCategory(ctx, pggen.LinkVideoCategoryParams{VideoID: videoID, CategoryID: categoryID})
}

func (a *PGAdapter) UpsertVideoCategorySpan(ctx context.Context, videoID int64, categoryID string, at time.Time) error {
	if _, err := a.db.Exec(ctx, upsertVideoCategorySpanSQL, videoID, categoryID, at.UTC()); err != nil {
		return fmt.Errorf("pg upsert video category span: %w", err)
	}
	return nil
}

func (a *PGAdapter) LinkStreamTag(ctx context.Context, streamID string, tagID int64) error {
	return a.queries.LinkStreamTag(ctx, pggen.LinkStreamTagParams{StreamID: streamID, TagID: tagID})
}

func (a *PGAdapter) LinkVideoTag(ctx context.Context, videoID, tagID int64) error {
	return a.queries.LinkVideoTag(ctx, pggen.LinkVideoTagParams{VideoID: videoID, TagID: tagID})
}

func (a *PGAdapter) ListCategoriesForVideo(ctx context.Context, videoID int64) ([]repository.CategorySpan, error) {
	rows, err := a.db.Query(ctx, listCategoriesForVideoSQL, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list categories for video: %w", err)
	}
	defer rows.Close()
	out := []repository.CategorySpan{}
	for rows.Next() {
		var c repository.CategorySpan
		if err := rows.Scan(
			&c.ID,
			&c.Name,
			&c.BoxArtURL,
			&c.IGDBID,
			&c.CreatedAt,
			&c.UpdatedAt,
			&c.StartedAt,
			&c.EndedAt,
			&c.DurationSeconds,
		); err != nil {
			return nil, fmt.Errorf("pg scan categories for video: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pg list categories for video: %w", err)
	}
	return out, nil
}

func (a *PGAdapter) ListPrimaryCategoriesForVideos(ctx context.Context, videoIDs []int64) (map[int64]repository.Category, error) {
	if len(videoIDs) == 0 {
		return map[int64]repository.Category{}, nil
	}
	rows, err := a.db.Query(ctx, listPrimaryCategoriesForVideosSQL, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("pg list primary categories for videos: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]repository.Category, len(videoIDs))
	for rows.Next() {
		var videoID int64
		var c repository.Category
		if err := rows.Scan(
			&videoID,
			&c.ID,
			&c.Name,
			&c.BoxArtURL,
			&c.IGDBID,
			&c.CreatedAt,
			&c.UpdatedAt,
			new(float64),
		); err != nil {
			return nil, fmt.Errorf("pg scan primary categories: %w", err)
		}
		out[videoID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pg list primary categories for videos: %w", err)
	}
	return out, nil
}

func (a *PGAdapter) ListTagsForVideo(ctx context.Context, videoID int64) ([]repository.Tag, error) {
	rows, err := a.queries.ListTagsForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list tags for video: %w", err)
	}
	out := make([]repository.Tag, len(rows))
	for i, r := range rows {
		out[i] = *pgTagToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) AddVideoRequest(ctx context.Context, videoID int64, userID string) error {
	return a.queries.AddVideoRequest(ctx, pggen.AddVideoRequestParams{VideoID: videoID, UserID: userID})
}

func (a *PGAdapter) ListVideoRequestsForUser(ctx context.Context, userID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideoRequestsForUser(ctx, pggen.ListVideoRequestsForUserParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list video requests: %w", err)
	}
	return pgVideosToDomain(rows), nil
}
