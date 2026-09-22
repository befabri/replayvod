package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) ListReadyVideoPlaybackAssets(ctx context.Context, after repository.PlaybackAssetCursor, limit int) ([]repository.VideoPlaybackAsset, error) {
	rows, err := a.queries.ListReadyVideoPlaybackAssets(ctx, pggen.ListReadyVideoPlaybackAssetsParams{AfterAccess: after.AccessedAt, AfterGenerated: after.GeneratedAt, AfterID: after.VideoID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list ready video playback assets: %w", err)
	}
	out := make([]repository.VideoPlaybackAsset, len(rows))
	for i, row := range rows {
		out[i] = *pgVideoPlaybackAssetToDomain(row)
	}
	return out, nil
}
