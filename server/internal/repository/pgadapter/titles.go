package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) UpsertVideoTitleSpan(ctx context.Context, videoID int64, titleID int64, at time.Time) error {
	if err := a.queries.UpsertVideoTitleSpan(ctx, pggen.UpsertVideoTitleSpanParams{
		VideoID: videoID,
		TitleID: titleID,
		At:      at.UTC(),
	}); err != nil {
		return fmt.Errorf("pg upsert video title span: %w", err)
	}
	return nil
}

func (a *PGAdapter) ListTitlesForVideo(ctx context.Context, videoID int64) ([]repository.TitleSpan, error) {
	rows, err := a.queries.ListTitleSpansForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list title spans for video: %w", err)
	}
	out := make([]repository.TitleSpan, len(rows))
	for i, r := range rows {
		out[i] = repository.TitleSpan{
			Title: repository.Title{
				ID:        r.ID,
				Name:      r.Name,
				CreatedAt: r.CreatedAt,
			},
			StartedAt:       r.StartedAt,
			EndedAt:         r.EndedAt,
			DurationSeconds: r.DurationSeconds,
		}
	}
	return out, nil
}
