package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetVideo(ctx context.Context, id int64) (*repository.Video, error) {
	row, err := a.queries.GetVideo(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgVideoToDomain(row), nil
}

func (a *PGAdapter) GetVideoByJobID(ctx context.Context, jobID string) (*repository.Video, error) {
	row, err := a.queries.GetVideoByJobID(ctx, jobID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgVideoToDomain(row), nil
}

func (a *PGAdapter) CreateVideo(ctx context.Context, v *repository.VideoInput) (*repository.Video, error) {
	row, err := a.queries.CreateVideo(ctx, pggen.CreateVideoParams{
		JobID:         v.JobID,
		Filename:      v.Filename,
		DisplayName:   v.DisplayName,
		Status:        v.Status,
		Quality:       v.Quality,
		BroadcasterID: v.BroadcasterID,
		StreamID:      v.StreamID,
		ViewerCount:   int32(v.ViewerCount),
		Language:      v.Language,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create video: %w", err)
	}
	return pgVideoToDomain(row), nil
}

func (a *PGAdapter) UpdateVideoStatus(ctx context.Context, id int64, status string) error {
	return a.queries.UpdateVideoStatus(ctx, pggen.UpdateVideoStatusParams{ID: id, Status: status})
}

func (a *PGAdapter) MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string) error {
	return a.queries.MarkVideoDone(ctx, pggen.MarkVideoDoneParams{
		ID:              id,
		DurationSeconds: &durationSeconds,
		SizeBytes:       &sizeBytes,
		Thumbnail:       thumbnail,
	})
}

func (a *PGAdapter) MarkVideoFailed(ctx context.Context, id int64, errMsg string) error {
	return a.queries.MarkVideoFailed(ctx, pggen.MarkVideoFailedParams{
		ID:    id,
		Error: &errMsg,
	})
}

func (a *PGAdapter) SetVideoThumbnail(ctx context.Context, id int64, thumbnail string) error {
	return a.queries.SetVideoThumbnail(ctx, pggen.SetVideoThumbnailParams{
		ID:        id,
		Thumbnail: &thumbnail,
	})
}

func (a *PGAdapter) ListVideos(ctx context.Context, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideos(ctx, pggen.ListVideosParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list videos: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListVideosByStatus(ctx context.Context, status string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosByStatus(ctx, pggen.ListVideosByStatusParams{
		Status: status,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list videos by status: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosByBroadcaster(ctx, pggen.ListVideosByBroadcasterParams{
		BroadcasterID: broadcasterID,
		Limit:         int32(limit),
		Offset:        int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list videos by broadcaster: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListVideosByCategory(ctx context.Context, categoryID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosByCategory(ctx, pggen.ListVideosByCategoryParams{
		CategoryID: categoryID,
		Limit:      int32(limit),
		Offset:     int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list videos by category: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListVideosMissingThumbnail(ctx context.Context) ([]repository.Video, error) {
	rows, err := a.queries.ListVideosMissingThumbnail(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list videos missing thumbnail: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) SoftDeleteVideo(ctx context.Context, id int64) error {
	return a.queries.SoftDeleteVideo(ctx, id)
}

func (a *PGAdapter) CountVideosByStatus(ctx context.Context, status string) (int64, error) {
	return a.queries.CountVideosByStatus(ctx, status)
}

func (a *PGAdapter) VideoStatsByStatus(ctx context.Context) ([]repository.VideoStatsByStatus, error) {
	rows, err := a.queries.StatisticsByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg video stats by status: %w", err)
	}
	out := make([]repository.VideoStatsByStatus, len(rows))
	for i, r := range rows {
		out[i] = repository.VideoStatsByStatus{Status: r.Status, Count: r.Count}
	}
	return out, nil
}

func (a *PGAdapter) VideoStatsTotals(ctx context.Context) (*repository.VideoStatsTotals, error) {
	row, err := a.queries.StatisticsTotals(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg video stats totals: %w", err)
	}
	return &repository.VideoStatsTotals{
		Total:         row.Total,
		TotalSize:     row.TotalSize,
		TotalDuration: row.TotalDuration,
	}, nil
}

func pgVideoToDomain(v pggen.Video) *repository.Video {
	return &repository.Video{
		ID:              v.ID,
		JobID:           v.JobID,
		Filename:        v.Filename,
		DisplayName:     v.DisplayName,
		Status:          v.Status,
		Quality:         v.Quality,
		BroadcasterID:   v.BroadcasterID,
		StreamID:        v.StreamID,
		ViewerCount:     int64(v.ViewerCount),
		Language:        v.Language,
		DurationSeconds: v.DurationSeconds,
		SizeBytes:       v.SizeBytes,
		Thumbnail:       v.Thumbnail,
		Error:           v.Error,
		StartDownloadAt: v.StartDownloadAt,
		DownloadedAt:    v.DownloadedAt,
		DeletedAt:       v.DeletedAt,
	}
}

func pgVideosToDomain(rows []pggen.Video) []repository.Video {
	out := make([]repository.Video, len(rows))
	for i, r := range rows {
		out[i] = *pgVideoToDomain(r)
	}
	return out
}
