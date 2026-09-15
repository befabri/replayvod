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
