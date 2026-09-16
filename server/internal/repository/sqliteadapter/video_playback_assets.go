package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) UpsertVideoPlaybackAsset(ctx context.Context, input *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	var sizeBytes sql.NullInt64
	if input.SizeBytes != nil {
		sizeBytes = sql.NullInt64{Int64: *input.SizeBytes, Valid: true}
	}
	row, err := a.queries.UpsertVideoPlaybackAsset(ctx, sqlitegen.UpsertVideoPlaybackAssetParams{
		VideoID:         input.VideoID,
		Status:          input.Status,
		Filename:        toNullString(input.Filename),
		MimeType:        toNullString(input.MimeType),
		DurationSeconds: nullFloat64(input.DurationSeconds),
		SizeBytes:       sizeBytes,
		Error:           toNullString(input.Error),
		GeneratedAt:     sqliteTimePtr(input.GeneratedAt),
		LastAccessedAt:  sqliteTimePtr(input.LastAccessedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert video playback asset: %w", err)
	}
	return sqliteVideoPlaybackAssetToDomain(row), nil
}

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
