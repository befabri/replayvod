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

func (a *SQLiteAdapter) TouchVideoPlaybackAsset(ctx context.Context, videoID int64) error {
	if err := a.queries.TouchVideoPlaybackAsset(ctx, videoID); err != nil {
		return fmt.Errorf("sqlite touch video playback asset: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) ListReadyVideoPlaybackAssets(ctx context.Context) ([]repository.VideoPlaybackAsset, error) {
	rows, err := a.queries.ListReadyVideoPlaybackAssets(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list ready video playback assets: %w", err)
	}
	out := make([]repository.VideoPlaybackAsset, len(rows))
	for i, row := range rows {
		out[i] = *sqliteVideoPlaybackAssetToDomain(row)
	}
	return out, nil
}

func (a *SQLiteAdapter) DeleteVideoPlaybackAsset(ctx context.Context, videoID int64) error {
	if err := a.queries.DeleteVideoPlaybackAsset(ctx, videoID); err != nil {
		return fmt.Errorf("sqlite delete video playback asset: %w", err)
	}
	return nil
}

func fromNullInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
