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

func (a *PGAdapter) GetVideoPart(ctx context.Context, id int64) (*repository.VideoPart, error) {
	row, err := a.queries.GetVideoPart(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgVideoPartToDomain(row), nil
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

func (a *PGAdapter) CountVideoParts(ctx context.Context, videoID int64) (int64, error) {
	return a.queries.CountVideoParts(ctx, videoID)
}

func (a *PGAdapter) DeleteVideoParts(ctx context.Context, videoID int64) error {
	return a.queries.DeleteVideoParts(ctx, videoID)
}

func pgVideoPartToDomain(p pggen.VideoPart) *repository.VideoPart {
	return &repository.VideoPart{
		ID:              p.ID,
		VideoID:         p.VideoID,
		PartIndex:       p.PartIndex,
		Filename:        p.Filename,
		Quality:         p.Quality,
		FPS:             p.Fps,
		Codec:           p.Codec,
		SegmentFormat:   p.SegmentFormat,
		DurationSeconds: p.DurationSeconds,
		SizeBytes:       p.SizeBytes,
		Thumbnail:       p.Thumbnail,
		StartMediaSeq:   p.StartMediaSeq,
		EndMediaSeq:     p.EndMediaSeq,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}
