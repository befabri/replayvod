package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

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

// UpsertVideoTitleSpan runs the close-previous-span + insert-new-span
// pair inside a tx so the two writes are atomic — SQLite has no
// equivalent to pg's single-CTE form, so we split into two sqlc
// queries and bracket them with BEGIN/COMMIT.
func (a *SQLiteAdapter) UpsertVideoTitleSpan(ctx context.Context, videoID int64, titleID int64, at time.Time) error {
	ts := formatTime(at.UTC())
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := q.CloseOtherOpenVideoTitleSpans(ctx, sqlitegen.CloseOtherOpenVideoTitleSpansParams{
			AtTime:  sql.NullString{String: ts, Valid: true},
			VideoID: videoID,
			TitleID: titleID,
		}); err != nil {
			return fmt.Errorf("sqlite close other open video title spans: %w", err)
		}
		if err := q.InsertVideoTitleSpan(ctx, sqlitegen.InsertVideoTitleSpanParams{
			VideoID: videoID,
			TitleID: titleID,
			AtTime:  ts,
		}); err != nil {
			return fmt.Errorf("sqlite insert video title span: %w", err)
		}
		return nil
	})
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
	rows, err := a.queries.ListTitleSpansForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list title spans for video: %w", err)
	}
	out := make([]repository.TitleSpan, len(rows))
	for i, r := range rows {
		span := repository.TitleSpan{
			Title: repository.Title{
				ID:        r.ID,
				Name:      r.Name,
				CreatedAt: parseTime(r.CreatedAt),
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

func sqliteTitleToDomain(t sqlitegen.Title) *repository.Title {
	return &repository.Title{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: parseTime(t.CreatedAt),
	}
}
