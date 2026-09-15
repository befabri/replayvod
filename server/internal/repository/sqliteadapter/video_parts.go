package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateVideoPart(ctx context.Context, input *repository.VideoPartInput) (*repository.VideoPart, error) {
	var fps sql.NullFloat64
	if input.FPS != nil {
		fps = sql.NullFloat64{Float64: *input.FPS, Valid: true}
	}
	row, err := a.queries.CreateVideoPart(ctx, sqlitegen.CreateVideoPartParams{
		VideoID:       input.VideoID,
		PartIndex:     int64(input.PartIndex),
		Filename:      input.Filename,
		Quality:       input.Quality,
		Fps:           fps,
		Codec:         input.Codec,
		SegmentFormat: input.SegmentFormat,
		StartMediaSeq: input.StartMediaSeq,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create video part: %w", err)
	}
	return sqliteVideoPartToDomain(row), nil
}

func (a *SQLiteAdapter) FinalizeVideoPart(ctx context.Context, input *repository.VideoPartFinalize) error {
	return a.queries.FinalizeVideoPart(ctx, sqlitegen.FinalizeVideoPartParams{
		ID:              input.ID,
		DurationSeconds: input.DurationSeconds,
		SizeBytes:       input.SizeBytes,
		Thumbnail:       toNullString(input.Thumbnail),
		EndMediaSeq:     sql.NullInt64{Int64: input.EndMediaSeq, Valid: true},
	})
}

func (a *SQLiteAdapter) ListVideoPartsForVideos(ctx context.Context, videoIDs []int64) ([]repository.VideoPart, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	rows, err := a.queries.ListVideoPartsForVideos(ctx, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("sqlite list video parts for videos: %w", err)
	}
	out := make([]repository.VideoPart, len(rows))
	for i, r := range rows {
		out[i] = *sqliteVideoPartToDomain(r)
	}
	return out, nil
}
