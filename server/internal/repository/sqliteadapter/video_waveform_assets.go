package sqliteadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetVideoWaveformKey(ctx context.Context, videoID int64) (string, error) {
	key, err := a.queries.GetVideoWaveformKey(ctx, videoID)
	return key, mapErr(err)
}

func (a *SQLiteAdapter) SetVideoWaveformKey(ctx context.Context, videoID int64, key string) error {
	return mapErr(a.queries.SetVideoWaveformKey(ctx, sqlitegen.SetVideoWaveformKeyParams{VideoID: videoID, Key: key}))
}

func (a *SQLiteAdapter) DeleteVideoWaveformKey(ctx context.Context, videoID int64) error {
	return mapErr(a.queries.DeleteVideoWaveformKey(ctx, videoID))
}
