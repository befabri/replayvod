package sqliteadapter

import (
	"context"
	"fmt"

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

func (a *SQLiteAdapter) ListTitlesForVideo(ctx context.Context, videoID int64) ([]repository.Title, error) {
	rows, err := a.queries.ListTitlesForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list titles for video: %w", err)
	}
	out := make([]repository.Title, len(rows))
	for i, r := range rows {
		out[i] = *sqliteTitleToDomain(r)
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
