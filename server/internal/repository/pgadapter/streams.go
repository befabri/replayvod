package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
)

func (a *PGAdapter) ListLatestLivePerChannel(ctx context.Context, limit int) ([]repository.LatestLiveStream, error) {
	rows, err := a.queries.ListLatestLivePerChannel(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("pg list latest live per channel: %w", err)
	}
	out := make([]repository.LatestLiveStream, len(rows))
	for i, r := range rows {
		out[i] = repository.LatestLiveStream{
			Stream: repository.Stream{
				ID:            r.ID,
				BroadcasterID: r.BroadcasterID,
				Type:          r.Type,
				Language:      r.Language,
				ThumbnailURL:  r.ThumbnailUrl,
				ViewerCount:   int64(r.ViewerCount),
				IsMature:      r.IsMature,
				StartedAt:     r.StartedAt,
				EndedAt:       r.EndedAt,
				CreatedAt:     r.CreatedAt,
			},
			BroadcasterLogin: r.BroadcasterLogin,
			BroadcasterName:  r.BroadcasterName,
			ProfileImageURL:  r.ProfileImageUrl,
		}
	}
	return out, nil
}
