package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/jackc/pgx/v5"
)

const listVideosByBroadcasterPageSQL = `SELECT
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind, truncated,
    trigger_schedule_id, retention_source_schedule_id, retention_window_hours
FROM videos
WHERE broadcaster_id = $1
  AND deleted_at IS NULL
  AND (
    $2::timestamptz IS NULL
    OR start_download_at < $2::timestamptz
    OR (start_download_at = $2::timestamptz AND id < $3)
  )
ORDER BY start_download_at DESC, id DESC
LIMIT $4`

const listVideosByCategoryPageSQL = `SELECT
    v.id, v.job_id, v.filename, v.display_name, v.status, v.quality, v.selected_quality,
    v.selected_fps, v.broadcaster_id, v.stream_id, v.viewer_count, v.language,
    v.duration_seconds, v.size_bytes, v.thumbnail, v.error,
    v.start_download_at, v.downloaded_at, v.deleted_at,
    v.recording_type, v.force_h264, v.title, v.completion_kind, v.truncated,
    v.trigger_schedule_id, v.retention_source_schedule_id, v.retention_window_hours
FROM videos v
INNER JOIN video_categories vc ON vc.video_id = v.id
WHERE vc.category_id = $1
  AND v.deleted_at IS NULL
  AND (
    $2::timestamptz IS NULL
    OR v.start_download_at < $2::timestamptz
    OR (v.start_download_at = $2::timestamptz AND v.id < $3)
  )
ORDER BY v.start_download_at DESC, v.id DESC
LIMIT $4`

const searchVideosSQL = `WITH q AS (
    SELECT
        lower($1::text) AS term,
        lower($1::text) || '%' AS prefix,
        '%' || lower($1::text) || '%' AS contains
),
matched AS (
    SELECT
        v.id, v.job_id, v.filename, v.display_name, v.status, v.quality, v.selected_quality,
        v.selected_fps, v.broadcaster_id, v.stream_id, v.viewer_count, v.language,
        v.duration_seconds, v.size_bytes, v.thumbnail, v.error,
        v.start_download_at, v.downloaded_at, v.deleted_at,
        v.recording_type, v.force_h264, v.title, v.completion_kind, v.truncated,
        v.trigger_schedule_id, v.retention_source_schedule_id, v.retention_window_hours,
        q.term = '' AS empty_query,
        lower(coalesce(v.title, '')) = q.term OR coalesce(title_match.title_exact, false) AS title_exact,
        lower(coalesce(v.title, '')) LIKE q.prefix OR coalesce(title_match.title_prefix, false) AS title_prefix,
        lower(coalesce(v.title, '')) LIKE q.contains OR coalesce(title_match.title_contains, false) AS title_contains,
        lower(coalesce(v.display_name, '')) = q.term
            OR lower(coalesce(ch.broadcaster_login, '')) = q.term
            OR lower(coalesce(ch.broadcaster_name, '')) = q.term AS channel_exact,
        lower(coalesce(v.display_name, '')) LIKE q.prefix
            OR lower(coalesce(ch.broadcaster_login, '')) LIKE q.prefix
            OR lower(coalesce(ch.broadcaster_name, '')) LIKE q.prefix AS channel_prefix,
        lower(coalesce(v.display_name, '')) LIKE q.contains
            OR lower(coalesce(ch.broadcaster_login, '')) LIKE q.contains
            OR lower(coalesce(ch.broadcaster_name, '')) LIKE q.contains AS channel_contains,
        coalesce(category_match.category_exact, false) AS category_exact,
        coalesce(category_match.category_prefix, false) AS category_prefix,
        coalesce(category_match.category_contains, false) AS category_contains
    FROM videos v
    CROSS JOIN q
    LEFT JOIN channels ch ON ch.broadcaster_id = v.broadcaster_id
    LEFT JOIN LATERAL (
        SELECT
            bool_or(lower(t.name) = q.term) AS title_exact,
            bool_or(lower(t.name) LIKE q.prefix) AS title_prefix,
            bool_or(lower(t.name) LIKE q.contains) AS title_contains
        FROM video_titles vt
        INNER JOIN titles t ON t.id = vt.title_id
        WHERE vt.video_id = v.id
    ) title_match ON true
    LEFT JOIN LATERAL (
        SELECT
            bool_or(lower(c.name) = q.term) AS category_exact,
            bool_or(lower(c.name) LIKE q.prefix) AS category_prefix,
            bool_or(lower(c.name) LIKE q.contains) AS category_contains
        FROM video_categories vc
        INNER JOIN categories c ON c.id = vc.category_id
        WHERE vc.video_id = v.id
    ) category_match ON true
    WHERE v.deleted_at IS NULL
)
SELECT
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind, truncated,
    trigger_schedule_id, retention_source_schedule_id, retention_window_hours
FROM matched
WHERE empty_query
   OR title_contains
   OR channel_contains
   OR category_contains
ORDER BY
    CASE
        WHEN empty_query THEN 7
        WHEN title_exact THEN 0
        WHEN title_prefix THEN 1
        WHEN channel_exact THEN 2
        WHEN channel_prefix THEN 3
        WHEN category_exact THEN 4
        WHEN category_prefix THEN 5
        ELSE 6
    END,
    start_download_at DESC,
    id DESC
LIMIT $2`

func (a *PGAdapter) CloseOpenVideoMetadataSpans(ctx context.Context, videoID int64, at time.Time) error {
	return closeOpenVideoMetadataSpansWith(ctx, a.queries, videoID, at)
}

func (a *PGAdapter) ResumeVideoMetadataSpans(ctx context.Context, videoID int64, at time.Time) error {
	at = at.UTC()
	if err := a.queries.ResumeVideoTitleSpan(ctx, pggen.ResumeVideoTitleSpanParams{
		VideoID: videoID,
		AtTime:  at,
	}); err != nil {
		return fmt.Errorf("pg resume video title spans: %w", err)
	}
	if err := a.queries.ResumeVideoCategorySpan(ctx, pggen.ResumeVideoCategorySpanParams{
		VideoID: videoID,
		AtTime:  at,
	}); err != nil {
		return fmt.Errorf("pg resume video category spans: %w", err)
	}
	return nil
}

// closeOpenVideoMetadataSpansWith runs both close queries against the
// supplied Queries handle. Separate from the PGAdapter method so
// MarkVideoDone/MarkVideoFailed can pass their tx-scoped Queries and
// share atomicity with the terminal video update that follows.
func closeOpenVideoMetadataSpansWith(ctx context.Context, q *pggen.Queries, videoID int64, at time.Time) error {
	at = at.UTC()
	if err := q.CloseOpenVideoTitleSpans(ctx, pggen.CloseOpenVideoTitleSpansParams{
		VideoID: videoID,
		AtTime:  at,
	}); err != nil {
		return fmt.Errorf("pg close video title spans: %w", err)
	}
	if err := q.CloseOpenVideoCategorySpans(ctx, pggen.CloseOpenVideoCategorySpansParams{
		VideoID: videoID,
		AtTime:  at,
	}); err != nil {
		return fmt.Errorf("pg close video category spans: %w", err)
	}
	return nil
}

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

func (a *PGAdapter) ListVideosByJobIDs(ctx context.Context, jobIDs []string) ([]repository.Video, error) {
	if len(jobIDs) == 0 {
		return []repository.Video{}, nil
	}
	rows, err := a.queries.ListVideosByJobIDs(ctx, jobIDs)
	if err != nil {
		return nil, fmt.Errorf("pg list videos by job ids: %w", err)
	}
	videos := make([]repository.Video, len(rows))
	for i, row := range rows {
		videos[i] = *pgVideoToDomain(row)
	}
	return videos, nil
}

func (a *PGAdapter) CreateVideo(ctx context.Context, v *repository.VideoInput) (*repository.Video, error) {
	rt := v.RecordingType
	if rt == "" {
		rt = repository.RecordingTypeVideo
	}
	row, err := a.queries.CreateVideo(ctx, pggen.CreateVideoParams{
		JobID:                     v.JobID,
		Filename:                  v.Filename,
		DisplayName:               v.DisplayName,
		Title:                     v.Title,
		Status:                    v.Status,
		Quality:                   v.Quality,
		BroadcasterID:             v.BroadcasterID,
		StreamID:                  v.StreamID,
		ViewerCount:               int32(v.ViewerCount),
		Language:                  v.Language,
		RecordingType:             rt,
		ForceH264:                 v.ForceH264,
		TriggerScheduleID:         v.TriggerScheduleID,
		RetentionSourceScheduleID: v.RetentionSourceScheduleID,
		RetentionWindowHours:      int64PtrToInt32Ptr(v.RetentionWindowHours),
	})
	if err != nil {
		return nil, fmt.Errorf("pg create video: %w", err)
	}
	return pgVideoToDomain(row), nil
}

func (a *PGAdapter) UpdateVideoStatus(ctx context.Context, id int64, status string) error {
	return a.queries.UpdateVideoStatus(ctx, pggen.UpdateVideoStatusParams{ID: id, Status: status})
}

func (a *PGAdapter) UpdateVideoSelectedVariant(ctx context.Context, id int64, quality string, fps *float64) error {
	var qualityPtr *string
	if quality != "" {
		qualityPtr = &quality
	}
	return a.queries.UpdateVideoSelectedVariant(ctx, pggen.UpdateVideoSelectedVariantParams{
		ID:              id,
		SelectedQuality: qualityPtr,
		SelectedFps:     fps,
	})
}

func (a *PGAdapter) MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string, truncated bool) error {
	return a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		return q.MarkVideoDone(ctx, pggen.MarkVideoDoneParams{
			ID:              id,
			DurationSeconds: &durationSeconds,
			SizeBytes:       &sizeBytes,
			Thumbnail:       thumbnail,
			CompletionKind:  completionKind,
			Truncated:       truncated,
		})
	})
}

func (a *PGAdapter) MarkVideoDoneAndEnqueueRecordingWebhook(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string, truncated bool, delivery *repository.RecordingWebhookDeliveryInput) error {
	return a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		if err := q.MarkVideoDone(ctx, pggen.MarkVideoDoneParams{
			ID:              id,
			DurationSeconds: &durationSeconds,
			SizeBytes:       &sizeBytes,
			Thumbnail:       thumbnail,
			CompletionKind:  completionKind,
			Truncated:       truncated,
		}); err != nil {
			return err
		}
		return pgCreateRecordingWebhookDeliveryIfEnabled(ctx, q, delivery)
	})
}

func (a *PGAdapter) MarkVideoFailed(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool) error {
	return a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		return q.MarkVideoFailed(ctx, pggen.MarkVideoFailedParams{
			ID:             id,
			Error:          &errMsg,
			CompletionKind: completionKind,
			Truncated:      truncated,
		})
	})
}

func (a *PGAdapter) MarkVideoFailedAndEnqueueRecordingWebhook(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool, delivery *repository.RecordingWebhookDeliveryInput) error {
	return a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		if err := q.MarkVideoFailed(ctx, pggen.MarkVideoFailedParams{
			ID:             id,
			Error:          &errMsg,
			CompletionKind: completionKind,
			Truncated:      truncated,
		}); err != nil {
			return err
		}
		return pgCreateRecordingWebhookDeliveryIfEnabled(ctx, q, delivery)
	})
}

func (a *PGAdapter) SetVideoThumbnail(ctx context.Context, id int64, thumbnail string) error {
	return a.queries.SetVideoThumbnail(ctx, pggen.SetVideoThumbnailParams{
		ID:        id,
		Thumbnail: &thumbnail,
	})
}

func (a *PGAdapter) ListVideos(ctx context.Context, opts repository.ListVideosOpts) ([]repository.Video, error) {
	rows, err := a.queries.ListVideos(ctx, pggen.ListVideosParams{
		StatusFilter: opts.Status,
		SortKey:      opts.SortKey(),
		RowOffset:    int32(opts.Offset),
		RowLimit:     int32(opts.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list videos: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListVideosPage(ctx context.Context, opts repository.ListVideosOpts, cursor *repository.VideoListPageCursor) (*repository.VideoListPage, error) {
	query, args := repository.BuildListVideosPageQuery(opts, cursor, repository.VideoPageDialect{
		Postgres:   true,
		FormatTime: func(t time.Time) any { return t },
	})
	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("pg list videos page: %w", err)
	}
	items, err := scanPGVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("pg list videos page: %w", err)
	}
	return repository.ToVideoListPage(items, opts), nil
}

func (a *PGAdapter) SearchVideos(ctx context.Context, query string, limit int) ([]repository.Video, error) {
	rows, err := a.db.Query(ctx, searchVideosSQL, query, limit)
	if err != nil {
		return nil, fmt.Errorf("pg search videos: %w", err)
	}
	items, err := scanPGVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("pg search videos: %w", err)
	}
	return items, nil
}

func (a *PGAdapter) ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.db.Query(ctx, listVideosByBroadcasterPageSQL,
		broadcasterID,
		pgCursorStartDownloadAt(cursor),
		pgCursorID(cursor),
		limit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("pg list videos by broadcaster: %w", err)
	}
	items, err := scanPGVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("pg list videos by broadcaster: %w", err)
	}
	return repository.ToVideoPage(items, limit), nil
}

func (a *PGAdapter) ListVideosByCategory(ctx context.Context, categoryID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.db.Query(ctx, listVideosByCategoryPageSQL,
		categoryID,
		pgCursorStartDownloadAt(cursor),
		pgCursorID(cursor),
		limit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("pg list videos by category: %w", err)
	}
	items, err := scanPGVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("pg list videos by category: %w", err)
	}
	return repository.ToVideoPage(items, limit), nil
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

func (a *PGAdapter) ListFinishedVideosForRetention(ctx context.Context, now time.Time) ([]repository.RetentionVideo, error) {
	rows, err := a.queries.ListFinishedVideosForRetention(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("pg list finished videos for retention: %w", err)
	}
	out := make([]repository.RetentionVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.RetentionVideo{
			VideoID:              r.ID,
			BroadcasterID:        r.BroadcasterID,
			DownloadedAt:         r.DownloadedAt,
			RetentionWindowHours: int32PtrToInt64Ptr(r.RetentionWindowHours),
		}
	}
	return out, nil
}

func (a *PGAdapter) FinalizeRetentionDelete(ctx context.Context, videoID int64) error {
	return a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		if err := q.SoftDeleteVideo(ctx, videoID); err != nil {
			return fmt.Errorf("pg retention tombstone video: %w", err)
		}
		if err := q.DeleteVideoParts(ctx, videoID); err != nil {
			return fmt.Errorf("pg retention delete parts: %w", err)
		}
		return nil
	})
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
		ThisWeek:      row.ThisWeek,
		Incomplete:    row.Incomplete,
		Channels:      row.Channels,
	}, nil
}

func (a *PGAdapter) VideoStatsTotalsByBroadcaster(ctx context.Context, broadcasterID string) (*repository.VideoStatsTotals, error) {
	row, err := a.queries.StatisticsTotalsByBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, fmt.Errorf("pg video stats totals by broadcaster: %w", err)
	}
	return &repository.VideoStatsTotals{
		Total:         row.Total,
		TotalSize:     row.TotalSize,
		TotalDuration: row.TotalDuration,
	}, nil
}

func pgVideoToDomain(v pggen.Video) *repository.Video {
	return &repository.Video{
		ID:                        v.ID,
		JobID:                     v.JobID,
		Filename:                  v.Filename,
		DisplayName:               v.DisplayName,
		Title:                     v.Title,
		Status:                    v.Status,
		Quality:                   v.Quality,
		SelectedQuality:           v.SelectedQuality,
		SelectedFPS:               v.SelectedFps,
		BroadcasterID:             v.BroadcasterID,
		StreamID:                  v.StreamID,
		ViewerCount:               int64(v.ViewerCount),
		Language:                  v.Language,
		DurationSeconds:           v.DurationSeconds,
		SizeBytes:                 v.SizeBytes,
		Thumbnail:                 v.Thumbnail,
		Error:                     v.Error,
		StartDownloadAt:           v.StartDownloadAt,
		DownloadedAt:              v.DownloadedAt,
		DeletedAt:                 v.DeletedAt,
		RecordingType:             v.RecordingType,
		ForceH264:                 v.ForceH264,
		TriggerScheduleID:         v.TriggerScheduleID,
		RetentionSourceScheduleID: v.RetentionSourceScheduleID,
		RetentionWindowHours:      int32PtrToInt64Ptr(v.RetentionWindowHours),
		CompletionKind:            v.CompletionKind,
		Truncated:                 v.Truncated,
	}
}

func pgVideosToDomain(rows []pggen.Video) []repository.Video {
	out := make([]repository.Video, len(rows))
	for i, r := range rows {
		out[i] = *pgVideoToDomain(r)
	}
	return out
}

func scanPGVideos(rows pgx.Rows) ([]repository.Video, error) {
	defer rows.Close()
	items := []repository.Video{}
	for rows.Next() {
		var row pggen.Video
		if err := rows.Scan(
			&row.ID,
			&row.JobID,
			&row.Filename,
			&row.DisplayName,
			&row.Status,
			&row.Quality,
			&row.SelectedQuality,
			&row.SelectedFps,
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
			&row.Truncated,
			&row.TriggerScheduleID,
			&row.RetentionSourceScheduleID,
			&row.RetentionWindowHours,
		); err != nil {
			return nil, err
		}
		items = append(items, *pgVideoToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func pgCursorStartDownloadAt(cursor *repository.VideoPageCursor) *time.Time {
	if cursor == nil {
		return nil
	}
	start := cursor.StartDownloadAt.UTC()
	return &start
}

func pgCursorID(cursor *repository.VideoPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.ID
}

// Pure page/cursor helpers now live in repository (pagination.go) so both
// adapters share one copy.
