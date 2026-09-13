package pgadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetVideoWaveformKey(ctx context.Context, videoID int64) (string, error) {
	key, err := a.queries.GetVideoWaveformKey(ctx, videoID)
	return key, mapErr(err)
}

func (a *PGAdapter) SetVideoWaveformKey(ctx context.Context, videoID int64, key string) error {
	return mapErr(a.queries.SetVideoWaveformKey(ctx, pggen.SetVideoWaveformKeyParams{VideoID: videoID, Key: key}))
}

func (a *PGAdapter) DeleteVideoWaveformKey(ctx context.Context, videoID int64) error {
	return mapErr(a.queries.DeleteVideoWaveformKey(ctx, videoID))
}
