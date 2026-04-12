package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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

func (a *PGAdapter) ListTitlesForVideo(ctx context.Context, videoID int64) ([]repository.Title, error) {
	rows, err := a.queries.ListTitlesForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list titles for video: %w", err)
	}
	out := make([]repository.Title, len(rows))
	for i, r := range rows {
		out[i] = *pgTitleToDomain(r)
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
