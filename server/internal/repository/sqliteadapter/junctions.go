package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

const touchVideoCategorySQL = `WITH now_ts(ts) AS (VALUES (?3))
UPDATE video_category_spans
   SET ended_at = ?3,
       duration_seconds = duration_seconds + ((julianday(?3) - julianday(started_at)) * 86400.0)
 WHERE video_id = ?1
   AND ended_at IS NULL
   AND category_id <> ?2;

INSERT INTO video_category_spans (video_id, category_id, started_at)
VALUES (?1, ?2, ?3)
ON CONFLICT (video_id, category_id) WHERE ended_at IS NULL DO NOTHING;`

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
        WHEN vcs.ended_at IS NULL THEN ((julianday('now') - julianday(vcs.started_at)) * 86400.0)
        ELSE 0
    END AS duration_seconds
FROM categories c
INNER JOIN video_category_spans vcs ON vcs.category_id = c.id
WHERE vcs.video_id = ?
ORDER BY vcs.started_at ASC, vcs.id ASC`

func (a *SQLiteAdapter) LinkStreamCategory(ctx context.Context, streamID, categoryID string) error {
	return a.queries.LinkStreamCategory(ctx, sqlitegen.LinkStreamCategoryParams{StreamID: streamID, CategoryID: categoryID})
}

func (a *SQLiteAdapter) LinkVideoCategory(ctx context.Context, videoID int64, categoryID string) error {
	return a.queries.LinkVideoCategory(ctx, sqlitegen.LinkVideoCategoryParams{VideoID: videoID, CategoryID: categoryID})
}

func (a *SQLiteAdapter) UpsertVideoCategorySpan(ctx context.Context, videoID int64, categoryID string, at time.Time) error {
	now := formatTime(at.UTC())
	if _, err := a.db.ExecContext(ctx, touchVideoCategorySQL, videoID, categoryID, now); err != nil {
		return fmt.Errorf("sqlite upsert video category span: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) LinkStreamTag(ctx context.Context, streamID string, tagID int64) error {
	return a.queries.LinkStreamTag(ctx, sqlitegen.LinkStreamTagParams{StreamID: streamID, TagID: tagID})
}

func (a *SQLiteAdapter) LinkVideoTag(ctx context.Context, videoID, tagID int64) error {
	return a.queries.LinkVideoTag(ctx, sqlitegen.LinkVideoTagParams{VideoID: videoID, TagID: tagID})
}

func (a *SQLiteAdapter) ListCategoriesForVideo(ctx context.Context, videoID int64) ([]repository.CategorySpan, error) {
	rows, err := a.db.QueryContext(ctx, listCategoriesForVideoSQL, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories for video: %w", err)
	}
	defer rows.Close()
	out := []repository.CategorySpan{}
	for rows.Next() {
		var c repository.CategorySpan
		var boxArt, igdb, endedAt sql.NullString
		var createdAt, updatedAt, startedAt string
		var duration sql.NullFloat64
		if err := rows.Scan(
			&c.ID,
			&c.Name,
			&boxArt,
			&igdb,
			&createdAt,
			&updatedAt,
			&startedAt,
			&endedAt,
			&duration,
		); err != nil {
			return nil, fmt.Errorf("sqlite scan categories for video: %w", err)
		}
		c.BoxArtURL = fromNullString(boxArt)
		c.IGDBID = fromNullString(igdb)
		c.CreatedAt = parseTime(createdAt)
		c.UpdatedAt = parseTime(updatedAt)
		c.StartedAt = parseTime(startedAt)
		if endedAt.Valid {
			v := parseTime(endedAt.String)
			c.EndedAt = &v
		}
		if duration.Valid {
			c.DurationSeconds = duration.Float64
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite list categories for video: %w", err)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListPrimaryCategoriesForVideos(ctx context.Context, videoIDs []int64) (map[int64]repository.Category, error) {
	if len(videoIDs) == 0 {
		return map[int64]repository.Category{}, nil
	}
	placeholders := make([]string, len(videoIDs))
	args := make([]any, len(videoIDs))
	for i, id := range videoIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `SELECT
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
	            WHEN vcs.ended_at IS NULL THEN ((julianday('now') - julianday(vcs.started_at)) * 86400.0)
	            ELSE 0
	        END) AS duration_seconds,
	        MIN(vcs.started_at) AS first_seen_at
	    FROM video_category_spans vcs
	    WHERE vcs.video_id IN (` + strings.Join(placeholders, ",") + `)
	    GROUP BY vcs.video_id, vcs.category_id
	) agg
	INNER JOIN categories c ON c.id = agg.category_id
	ORDER BY agg.video_id ASC, duration_seconds DESC, agg.first_seen_at ASC, c.name ASC`
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite list primary categories for videos: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]repository.Category, len(videoIDs))
	for rows.Next() {
		var videoID int64
		var c repository.Category
		var boxArt, igdb sql.NullString
		var createdAt, updatedAt string
		var duration float64
		if err := rows.Scan(
			&videoID,
			&c.ID,
			&c.Name,
			&boxArt,
			&igdb,
			&createdAt,
			&updatedAt,
			&duration,
		); err != nil {
			return nil, fmt.Errorf("sqlite scan primary categories: %w", err)
		}
		if _, exists := out[videoID]; exists {
			continue
		}
		c.BoxArtURL = fromNullString(boxArt)
		c.IGDBID = fromNullString(igdb)
		c.CreatedAt = parseTime(createdAt)
		c.UpdatedAt = parseTime(updatedAt)
		out[videoID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite list primary categories for videos: %w", err)
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
