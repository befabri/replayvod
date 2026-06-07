package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateVideoPart(ctx context.Context, input *repository.VideoPartInput) (*repository.VideoPart, error) {
	row, err := a.queries.CreateVideoPart(ctx, pggen.CreateVideoPartParams{
		VideoID:       input.VideoID,
		PartIndex:     input.PartIndex,
		Filename:      input.Filename,
		Quality:       input.Quality,
		Fps:           input.FPS,
		Codec:         input.Codec,
		SegmentFormat: input.SegmentFormat,
		StartMediaSeq: input.StartMediaSeq,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create video part: %w", err)
	}
	return pgVideoPartToDomain(row), nil
}

func (a *PGAdapter) FinalizeVideoPart(ctx context.Context, input *repository.VideoPartFinalize) error {
	end := input.EndMediaSeq
	return a.queries.FinalizeVideoPart(ctx, pggen.FinalizeVideoPartParams{
		ID:              input.ID,
		DurationSeconds: input.DurationSeconds,
		SizeBytes:       input.SizeBytes,
		Thumbnail:       input.Thumbnail,
		EndMediaSeq:     &end,
	})
}

func (a *PGAdapter) GetVideoPartByIndex(ctx context.Context, videoID int64, partIndex int32) (*repository.VideoPart, error) {
	row, err := a.queries.GetVideoPartByIndex(ctx, pggen.GetVideoPartByIndexParams{
		VideoID:   videoID,
		PartIndex: partIndex,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgVideoPartToDomain(row), nil
}

func (a *PGAdapter) ListVideoParts(ctx context.Context, videoID int64) ([]repository.VideoPart, error) {
	rows, err := a.queries.ListVideoParts(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list video parts: %w", err)
	}
	out := make([]repository.VideoPart, len(rows))
	for i, r := range rows {
		out[i] = *pgVideoPartToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) ListVideoPartsForVideos(ctx context.Context, videoIDs []int64) ([]repository.VideoPart, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	rows, err := a.queries.ListVideoPartsForVideos(ctx, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("pg list video parts for videos: %w", err)
	}
	out := make([]repository.VideoPart, len(rows))
	for i, r := range rows {
		out[i] = *pgVideoPartToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) CountVideoParts(ctx context.Context, videoID int64) (int64, error) {
	return a.queries.CountVideoParts(ctx, videoID)
}

func (a *PGAdapter) HasFinalizedVideoParts(ctx context.Context, videoID int64) (bool, error) {
	return a.queries.HasFinalizedVideoParts(ctx, videoID)
}
