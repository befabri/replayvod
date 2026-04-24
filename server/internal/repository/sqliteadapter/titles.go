package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

const touchVideoTitleSQL = `WITH now_ts(ts) AS (VALUES (?3))
UPDATE video_title_spans
   SET ended_at = ?3,
       duration_seconds = duration_seconds + ((julianday(?3) - julianday(started_at)) * 86400.0)
 WHERE video_id = ?1
   AND ended_at IS NULL
   AND title_id <> ?2;

INSERT INTO video_title_spans (video_id, title_id, started_at)
VALUES (?1, ?2, ?3)
ON CONFLICT (video_id, title_id) WHERE ended_at IS NULL DO NOTHING;`

const listTitlesForVideoSQL = `SELECT
    t.id,
    t.name,
    t.created_at,
    vts.started_at,
    vts.ended_at,
    vts.duration_seconds + CASE
        WHEN vts.ended_at IS NULL THEN ((julianday('now') - julianday(vts.started_at)) * 86400.0)
        ELSE 0
    END AS duration_seconds
FROM titles t
INNER JOIN video_title_spans vts ON vts.title_id = t.id
WHERE vts.video_id = ?
ORDER BY vts.started_at ASC, vts.id ASC`

func (a *SQLiteAdapter) UpsertTitle(ctx context.Context, name string) (*repository.Title, error) {
	row, err := a.queries.UpsertTitle(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert title: %w", err)
	}
	return sqliteTitleToDomain(row), nil
}

func (a *SQLiteAdapter) LinkStreamTitle(ctx context.Context, streamID string, titleID int64) error {
	return a.queries.LinkStreamTitle(ctx, sqlitegen.LinkStreamTitleParams{StreamID: streamID, TitleID: titleID})
}

func (a *SQLiteAdapter) LinkVideoTitle(ctx context.Context, videoID int64, titleID int64) error {
	return a.queries.LinkVideoTitle(ctx, sqlitegen.LinkVideoTitleParams{VideoID: videoID, TitleID: titleID})
}

func (a *SQLiteAdapter) UpsertVideoTitleSpan(ctx context.Context, videoID int64, titleID int64, at time.Time) error {
	now := formatTime(at.UTC())
	if _, err := a.db.ExecContext(ctx, touchVideoTitleSQL, videoID, titleID, now); err != nil {
		return fmt.Errorf("sqlite upsert video title span: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) ListTitlesForStream(ctx context.Context, streamID string) ([]repository.Title, error) {
	rows, err := a.queries.ListTitlesForStream(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list titles for stream: %w", err)
	}
	out := make([]repository.Title, len(rows))
	for i, r := range rows {
		out[i] = *sqliteTitleToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListTitlesForVideo(ctx context.Context, videoID int64) ([]repository.TitleSpan, error) {
	rows, err := a.db.QueryContext(ctx, listTitlesForVideoSQL, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list titles for video: %w", err)
	}
	defer rows.Close()
	out := []repository.TitleSpan{}
	for rows.Next() {
		var title repository.TitleSpan
		var endedAt sql.NullString
		var duration sql.NullFloat64
		var createdAt, startedAt string
		if err := rows.Scan(&title.ID, &title.Name, &createdAt, &startedAt, &endedAt, &duration); err != nil {
			return nil, fmt.Errorf("sqlite scan titles for video: %w", err)
		}
		title.CreatedAt = parseTime(createdAt)
		title.StartedAt = parseTime(startedAt)
		if endedAt.Valid {
			v := parseTime(endedAt.String)
			title.EndedAt = &v
		}
		if duration.Valid {
			title.DurationSeconds = duration.Float64
		}
		out = append(out, title)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite list titles for video: %w", err)
	}
	return out, nil
}

func sqliteTitleToDomain(t sqlitegen.Title) *repository.Title {
	return &repository.Title{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: parseTime(t.CreatedAt),
	}
}
