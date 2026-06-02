package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetVideoPlaybackAsset(ctx context.Context, videoID int64) (*repository.VideoPlaybackAsset, error) {
	row, err := a.queries.GetVideoPlaybackAsset(ctx, videoID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoPlaybackAssetToDomain(row), nil
}

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
		GeneratedAt:     nullTimeString(input.GeneratedAt),
		LastAccessedAt:  nullTimeString(input.LastAccessedAt),
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

func sqliteVideoPlaybackAssetToDomain(a sqlitegen.VideoPlaybackAsset) *repository.VideoPlaybackAsset {
	return &repository.VideoPlaybackAsset{
		VideoID:         a.VideoID,
		Status:          a.Status,
		Filename:        fromNullString(a.Filename),
		MimeType:        fromNullString(a.MimeType),
		DurationSeconds: fromNullFloat64(a.DurationSeconds),
		SizeBytes:       fromNullInt64(a.SizeBytes),
		Error:           fromNullString(a.Error),
		GeneratedAt:     fromNullTimeString(a.GeneratedAt),
		LastAccessedAt:  fromNullTimeString(a.LastAccessedAt),
		CreatedAt:       parseTime(a.CreatedAt),
		UpdatedAt:       parseTime(a.UpdatedAt),
	}
}

func nullTimeString(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*t), Valid: true}
}

func fromNullInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

func fromNullTimeString(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t := parseTime(s.String)
	return &t
}
