package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetVideoPlaybackAsset(ctx context.Context, videoID int64) (*repository.VideoPlaybackAsset, error) {
	row, err := a.queries.GetVideoPlaybackAsset(ctx, videoID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgVideoPlaybackAssetToDomain(row), nil
}

func (a *PGAdapter) UpsertVideoPlaybackAsset(ctx context.Context, input *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	row, err := a.queries.UpsertVideoPlaybackAsset(ctx, pggen.UpsertVideoPlaybackAssetParams{
		VideoID:         input.VideoID,
		Status:          input.Status,
		Filename:        input.Filename,
		MimeType:        input.MimeType,
		DurationSeconds: input.DurationSeconds,
		SizeBytes:       input.SizeBytes,
		Error:           input.Error,
		GeneratedAt:     input.GeneratedAt,
		LastAccessedAt:  input.LastAccessedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert video playback asset: %w", err)
	}
	return pgVideoPlaybackAssetToDomain(row), nil
}

func (a *PGAdapter) TouchVideoPlaybackAsset(ctx context.Context, videoID int64) error {
	if err := a.queries.TouchVideoPlaybackAsset(ctx, videoID); err != nil {
		return fmt.Errorf("pg touch video playback asset: %w", err)
	}
	return nil
}

func (a *PGAdapter) ListReadyVideoPlaybackAssets(ctx context.Context) ([]repository.VideoPlaybackAsset, error) {
	rows, err := a.queries.ListReadyVideoPlaybackAssets(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list ready video playback assets: %w", err)
	}
	out := make([]repository.VideoPlaybackAsset, len(rows))
	for i, row := range rows {
		out[i] = *pgVideoPlaybackAssetToDomain(row)
	}
	return out, nil
}

func (a *PGAdapter) DeleteVideoPlaybackAsset(ctx context.Context, videoID int64) error {
	if err := a.queries.DeleteVideoPlaybackAsset(ctx, videoID); err != nil {
		return fmt.Errorf("pg delete video playback asset: %w", err)
	}
	return nil
}

func pgVideoPlaybackAssetToDomain(a pggen.VideoPlaybackAsset) *repository.VideoPlaybackAsset {
	return &repository.VideoPlaybackAsset{
		VideoID:         a.VideoID,
		Status:          a.Status,
		Filename:        a.Filename,
		MimeType:        a.MimeType,
		DurationSeconds: a.DurationSeconds,
		SizeBytes:       a.SizeBytes,
		Error:           a.Error,
		GeneratedAt:     a.GeneratedAt,
		LastAccessedAt:  a.LastAccessedAt,
		CreatedAt:       a.CreatedAt,
		UpdatedAt:       a.UpdatedAt,
	}
}
