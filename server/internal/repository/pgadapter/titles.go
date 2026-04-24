package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

const upsertVideoTitleSpanSQL = `WITH close_previous AS (
    UPDATE video_title_spans vts
       SET ended_at = $3,
           duration_seconds = vts.duration_seconds + EXTRACT(EPOCH FROM ($3 - vts.started_at))
     WHERE vts.video_id = $1
       AND vts.ended_at IS NULL
       AND vts.title_id <> $2
)
INSERT INTO video_title_spans (video_id, title_id, started_at)
VALUES ($1, $2, $3)
ON CONFLICT (video_id, title_id) WHERE ended_at IS NULL DO NOTHING`

const listTitlesForVideoSQL = `SELECT
    t.id,
    t.name,
    t.created_at,
    vts.started_at,
    vts.ended_at,
    vts.duration_seconds + CASE
        WHEN vts.ended_at IS NULL THEN EXTRACT(EPOCH FROM (NOW() - vts.started_at))
        ELSE 0
    END AS duration_seconds
FROM titles t
INNER JOIN video_title_spans vts ON vts.title_id = t.id
WHERE vts.video_id = $1
ORDER BY vts.started_at ASC, vts.id ASC`

func (a *PGAdapter) UpsertTitle(ctx context.Context, name string) (*repository.Title, error) {
	row, err := a.queries.UpsertTitle(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("pg upsert title: %w", err)
	}
	return pgTitleToDomain(row), nil
}

func (a *PGAdapter) LinkStreamTitle(ctx context.Context, streamID string, titleID int64) error {
	return a.queries.LinkStreamTitle(ctx, pggen.LinkStreamTitleParams{StreamID: streamID, TitleID: titleID})
}

func (a *PGAdapter) LinkVideoTitle(ctx context.Context, videoID int64, titleID int64) error {
	return a.queries.LinkVideoTitle(ctx, pggen.LinkVideoTitleParams{VideoID: videoID, TitleID: titleID})
}

func (a *PGAdapter) UpsertVideoTitleSpan(ctx context.Context, videoID int64, titleID int64, at time.Time) error {
	if _, err := a.db.Exec(ctx, upsertVideoTitleSpanSQL, videoID, titleID, at.UTC()); err != nil {
		return fmt.Errorf("pg upsert video title span: %w", err)
	}
	return nil
}

func (a *PGAdapter) ListTitlesForStream(ctx context.Context, streamID string) ([]repository.Title, error) {
	rows, err := a.queries.ListTitlesForStream(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("pg list titles for stream: %w", err)
	}
	out := make([]repository.Title, len(rows))
	for i, r := range rows {
		out[i] = *pgTitleToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) ListTitlesForVideo(ctx context.Context, videoID int64) ([]repository.TitleSpan, error) {
	rows, err := a.db.Query(ctx, listTitlesForVideoSQL, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list titles for video: %w", err)
	}
	defer rows.Close()
	out := []repository.TitleSpan{}
	for rows.Next() {
		var t repository.TitleSpan
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.StartedAt, &t.EndedAt, &t.DurationSeconds); err != nil {
			return nil, fmt.Errorf("pg scan titles for video: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pg list titles for video: %w", err)
	}
	return out, nil
}

func pgTitleToDomain(t pggen.Title) *repository.Title {
	return &repository.Title{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt,
	}
}
