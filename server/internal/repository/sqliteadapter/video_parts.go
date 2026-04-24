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

func (a *SQLiteAdapter) GetVideoPart(ctx context.Context, id int64) (*repository.VideoPart, error) {
	row, err := a.queries.GetVideoPart(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoPartToDomain(row), nil
}

func (a *SQLiteAdapter) GetVideoPartByIndex(ctx context.Context, videoID int64, partIndex int32) (*repository.VideoPart, error) {
	row, err := a.queries.GetVideoPartByIndex(ctx, sqlitegen.GetVideoPartByIndexParams{
		VideoID:   videoID,
		PartIndex: int64(partIndex),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoPartToDomain(row), nil
}

func (a *SQLiteAdapter) ListVideoParts(ctx context.Context, videoID int64) ([]repository.VideoPart, error) {
	rows, err := a.queries.ListVideoParts(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list video parts: %w", err)
	}
	out := make([]repository.VideoPart, len(rows))
	for i, r := range rows {
		out[i] = *sqliteVideoPartToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) CountVideoParts(ctx context.Context, videoID int64) (int64, error) {
	return a.queries.CountVideoParts(ctx, videoID)
}

func (a *SQLiteAdapter) DeleteVideoParts(ctx context.Context, videoID int64) error {
	return a.queries.DeleteVideoParts(ctx, videoID)
}

func sqliteVideoPartToDomain(p sqlitegen.VideoPart) *repository.VideoPart {
	var endMediaSeq *int64
	if p.EndMediaSeq.Valid {
		v := p.EndMediaSeq.Int64
		endMediaSeq = &v
	}
	var fps *float64
	if p.Fps.Valid {
		v := p.Fps.Float64
		fps = &v
	}
	return &repository.VideoPart{
		ID:              p.ID,
		VideoID:         p.VideoID,
		PartIndex:       int32(p.PartIndex),
		Filename:        p.Filename,
		Quality:         p.Quality,
		FPS:             fps,
		Codec:           p.Codec,
		SegmentFormat:   p.SegmentFormat,
		DurationSeconds: p.DurationSeconds,
		SizeBytes:       p.SizeBytes,
		Thumbnail:       fromNullString(p.Thumbnail),
		StartMediaSeq:   p.StartMediaSeq,
		EndMediaSeq:     endMediaSeq,
		CreatedAt:       parseTime(p.CreatedAt),
		UpdatedAt:       parseTime(p.UpdatedAt),
	}
}
