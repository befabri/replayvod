package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) ListReadyVideoPlaybackAssets(ctx context.Context, after repository.PlaybackAssetCursor, limit int) ([]repository.VideoPlaybackAsset, error) {
	rows, err := a.queries.ListReadyVideoPlaybackAssets(ctx, sqlitegen.ListReadyVideoPlaybackAssetsParams{AfterAccess: sqliteTimePtr(&after.AccessedAt), AfterGenerated: sqliteTimePtr(&after.GeneratedAt), AfterID: after.VideoID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list ready video playback assets: %w", err)
	}
	out := make([]repository.VideoPlaybackAsset, len(rows))
	for i, row := range rows {
		out[i] = *sqliteVideoPlaybackAssetToDomain(row)
	}
	return out, nil
}
