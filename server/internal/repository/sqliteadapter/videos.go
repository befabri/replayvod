package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetVideo(ctx context.Context, id int64) (*repository.Video, error) {
	row, err := a.queries.GetVideo(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoToDomain(row), nil
}

func (a *SQLiteAdapter) GetVideoByJobID(ctx context.Context, jobID string) (*repository.Video, error) {
	row, err := a.queries.GetVideoByJobID(ctx, jobID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoToDomain(row), nil
}

func (a *SQLiteAdapter) CreateVideo(ctx context.Context, v *repository.VideoInput) (*repository.Video, error) {
	row, err := a.queries.CreateVideo(ctx, sqlitegen.CreateVideoParams{
		JobID:         v.JobID,
		Filename:      v.Filename,
		DisplayName:   v.DisplayName,
		Status:        v.Status,
		Quality:       v.Quality,
		BroadcasterID: v.BroadcasterID,
		StreamID:      toNullString(v.StreamID),
		ViewerCount:   v.ViewerCount,
		Language:      v.Language,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create video: %w", err)
	}
	return sqliteVideoToDomain(row), nil
}

func (a *SQLiteAdapter) UpdateVideoStatus(ctx context.Context, id int64, status string) error {
	return a.queries.UpdateVideoStatus(ctx, sqlitegen.UpdateVideoStatusParams{ID: id, Status: status})
}

func (a *SQLiteAdapter) MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string) error {
	return a.queries.MarkVideoDone(ctx, sqlitegen.MarkVideoDoneParams{
		ID:              id,
		DurationSeconds: sql.NullFloat64{Float64: durationSeconds, Valid: true},
		SizeBytes:       sql.NullInt64{Int64: sizeBytes, Valid: true},
		Thumbnail:       toNullString(thumbnail),
	})
}

func (a *SQLiteAdapter) MarkVideoFailed(ctx context.Context, id int64, errMsg string) error {
	return a.queries.MarkVideoFailed(ctx, sqlitegen.MarkVideoFailedParams{
		ID:    id,
		Error: sql.NullString{String: errMsg, Valid: true},
	})
}

func (a *SQLiteAdapter) SetVideoThumbnail(ctx context.Context, id int64, thumbnail string) error {
	return a.queries.SetVideoThumbnail(ctx, sqlitegen.SetVideoThumbnailParams{
		ID:        id,
		Thumbnail: sql.NullString{String: thumbnail, Valid: true},
	})
}

func (a *SQLiteAdapter) ListVideos(ctx context.Context, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideos(ctx, sqlitegen.ListVideosParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) ListVideosByStatus(ctx context.Context, status string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosByStatus(ctx, sqlitegen.ListVideosByStatusParams{
		Status: status,
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by status: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosByBroadcaster(ctx, sqlitegen.ListVideosByBroadcasterParams{
		BroadcasterID: broadcasterID,
		Limit:         int64(limit),
		Offset:        int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by broadcaster: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) ListVideosByCategory(ctx context.Context, categoryID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosByCategory(ctx, sqlitegen.ListVideosByCategoryParams{
		CategoryID: categoryID,
		Limit:      int64(limit),
		Offset:     int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by category: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) ListVideosMissingThumbnail(ctx context.Context) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosMissingThumbnail(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos missing thumbnail: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) SoftDeleteVideo(ctx context.Context, id int64) error {
	return a.queries.SoftDeleteVideo(ctx, id)
}

func (a *SQLiteAdapter) CountVideosByStatus(ctx context.Context, status string) (int64, error) {
	return a.queries.CountVideosByStatus(ctx, status)
}

func (a *SQLiteAdapter) VideoStatsByStatus(ctx context.Context) ([]repository.VideoStatsByStatus, error) {
	rows, err := a.queries.StatisticsByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats by status: %w", err)
	}
	out := make([]repository.VideoStatsByStatus, len(rows))
	for i, r := range rows {
		out[i] = repository.VideoStatsByStatus{Status: r.Status, Count: r.Count}
	}
	return out, nil
}

func (a *SQLiteAdapter) VideoStatsTotals(ctx context.Context) (*repository.VideoStatsTotals, error) {
	row, err := a.queries.StatisticsTotals(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals: %w", err)
	}
	return &repository.VideoStatsTotals{
		Total:         row.Total,
		TotalSize:     row.TotalSize,
		TotalDuration: row.TotalDuration,
	}, nil
}

func sqliteVideoToDomain(v sqlitegen.Video) *repository.Video {
	var duration *float64
	if v.DurationSeconds.Valid {
		f := v.DurationSeconds.Float64
		duration = &f
	}
	var size *int64
	if v.SizeBytes.Valid {
		i := v.SizeBytes.Int64
		size = &i
	}
	return &repository.Video{
		ID:              v.ID,
		JobID:           v.JobID,
		Filename:        v.Filename,
		DisplayName:     v.DisplayName,
		Status:          v.Status,
		Quality:         v.Quality,
		BroadcasterID:   v.BroadcasterID,
		StreamID:        fromNullString(v.StreamID),
		ViewerCount:     v.ViewerCount,
		Language:        v.Language,
		DurationSeconds: duration,
		SizeBytes:       size,
		Thumbnail:       fromNullString(v.Thumbnail),
		Error:           fromNullString(v.Error),
		StartDownloadAt: parseTime(v.StartDownloadAt),
		DownloadedAt:    parseNullTime(v.DownloadedAt),
		DeletedAt:       parseNullTime(v.DeletedAt),
	}
}

func sqliteVideosToDomain(rows []sqlitegen.Video) []repository.Video {
	out := make([]repository.Video, len(rows))
	for i, r := range rows {
		out[i] = *sqliteVideoToDomain(r)
	}
	return out
}
