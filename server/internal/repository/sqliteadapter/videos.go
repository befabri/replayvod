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
	rt := v.RecordingType
	if rt == "" {
		rt = repository.RecordingTypeVideo
	}
	var force int64
	if v.ForceH264 {
		force = 1
	}
	row, err := a.queries.CreateVideo(ctx, sqlitegen.CreateVideoParams{
		JobID:         v.JobID,
		Filename:      v.Filename,
		DisplayName:   v.DisplayName,
		Title:         v.Title,
		Status:        v.Status,
		Quality:       v.Quality,
		BroadcasterID: v.BroadcasterID,
		StreamID:      toNullString(v.StreamID),
		ViewerCount:   v.ViewerCount,
		Language:      v.Language,
		RecordingType: rt,
		ForceH264:     force,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create video: %w", err)
	}
	return sqliteVideoToDomain(row), nil
}

func (a *SQLiteAdapter) UpdateVideoStatus(ctx context.Context, id int64, status string) error {
	return a.queries.UpdateVideoStatus(ctx, sqlitegen.UpdateVideoStatusParams{ID: id, Status: status})
}

func (a *SQLiteAdapter) MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string) error {
	return a.queries.MarkVideoDone(ctx, sqlitegen.MarkVideoDoneParams{
		ID:              id,
		DurationSeconds: sql.NullFloat64{Float64: durationSeconds, Valid: true},
		SizeBytes:       sql.NullInt64{Int64: sizeBytes, Valid: true},
		Thumbnail:       toNullString(thumbnail),
		CompletionKind:  completionKind,
	})
}

func (a *SQLiteAdapter) MarkVideoFailed(ctx context.Context, id int64, errMsg string, completionKind string) error {
	return a.queries.MarkVideoFailed(ctx, sqlitegen.MarkVideoFailedParams{
		ID:             id,
		Error:          sql.NullString{String: errMsg, Valid: true},
		CompletionKind: completionKind,
	})
}

func (a *SQLiteAdapter) SetVideoThumbnail(ctx context.Context, id int64, thumbnail string) error {
	return a.queries.SetVideoThumbnail(ctx, sqlitegen.SetVideoThumbnailParams{
		ID:        id,
		Thumbnail: sql.NullString{String: thumbnail, Valid: true},
	})
}

// listVideosSQL mirrors queries/postgres/videos.sql ListVideos. Hand-
// rolled because sqlc's SQLite engine can't type-infer a ?N param whose
// only usages are inside a CASE expression (see queries/sqlite/videos.sql).
// Parameter positions: ?1 status_filter ("" = unfiltered), ?2 sort_key
// ("duration-desc", "channel-asc", …; empty/unrecognized = default
// created-desc), ?3 row_limit, ?4 row_offset. The trailing
// start_download_at DESC is both the explicit 'created_at-desc' sort
// and the fallthrough for empty/unrecognized sort_key values.
const listVideosSQL = `SELECT
    id, job_id, filename, display_name, status, quality,
    broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind
FROM videos
WHERE deleted_at IS NULL
  AND (?1 = '' OR status = ?1)
ORDER BY
  CASE WHEN ?2 = 'duration-desc'  THEN duration_seconds  END DESC NULLS LAST,
  CASE WHEN ?2 = 'duration-asc'   THEN duration_seconds  END ASC NULLS LAST,
  CASE WHEN ?2 = 'size-desc'      THEN size_bytes        END DESC NULLS LAST,
  CASE WHEN ?2 = 'size-asc'       THEN size_bytes        END ASC NULLS LAST,
  CASE WHEN ?2 = 'channel-asc'    THEN display_name      END ASC,
  CASE WHEN ?2 = 'channel-desc'   THEN display_name      END DESC,
  CASE WHEN ?2 = 'created_at-asc' THEN start_download_at END ASC,
  start_download_at DESC,
  -- Tiebreaker direction tracks the primary sort intent; see
  -- queries/postgres/videos.sql ListVideos for rationale. Kept in
  -- sync between dialects so the ordering contract doesn't diverge.
  CASE WHEN ?2 LIKE '%-asc' THEN id END ASC,
  id DESC
LIMIT ?3 OFFSET ?4`

func (a *SQLiteAdapter) ListVideos(ctx context.Context, opts repository.ListVideosOpts) ([]repository.Video, error) {
	rows, err := a.db.QueryContext(ctx, listVideosSQL,
		opts.Status,
		opts.SortKey(),
		int64(opts.Limit),
		int64(opts.Offset),
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos: %w", err)
	}
	defer rows.Close()
	out := []repository.Video{}
	for rows.Next() {
		var row sqlitegen.Video
		if err := rows.Scan(
			&row.ID,
			&row.JobID,
			&row.Filename,
			&row.DisplayName,
			&row.Status,
			&row.Quality,
			&row.BroadcasterID,
			&row.StreamID,
			&row.ViewerCount,
			&row.Language,
			&row.DurationSeconds,
			&row.SizeBytes,
			&row.Thumbnail,
			&row.Error,
			&row.StartDownloadAt,
			&row.DownloadedAt,
			&row.DeletedAt,
			&row.RecordingType,
			&row.ForceH264,
			&row.Title,
			&row.CompletionKind,
		); err != nil {
			return nil, fmt.Errorf("sqlite list videos scan: %w", err)
		}
		out = append(out, *sqliteVideoToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite list videos: %w", err)
	}
	return out, nil
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
		Title:           v.Title,
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
		RecordingType:   v.RecordingType,
		ForceH264:       v.ForceH264 != 0,
		CompletionKind:  v.CompletionKind,
	}
}

func sqliteVideosToDomain(rows []sqlitegen.Video) []repository.Video {
	out := make([]repository.Video, len(rows))
	for i, r := range rows {
		out[i] = *sqliteVideoToDomain(r)
	}
	return out
}
