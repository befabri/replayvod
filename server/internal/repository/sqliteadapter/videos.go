package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CloseOpenVideoMetadataSpans(ctx context.Context, videoID int64, at time.Time) error {
	return closeOpenVideoMetadataSpansWith(ctx, a.queries, videoID, at)
}

func (a *SQLiteAdapter) ResumeVideoMetadataSpans(ctx context.Context, videoID int64, at time.Time) error {
	ts := sqliteTime(at)
	if err := a.queries.ResumeVideoTitleSpan(ctx, sqlitegen.ResumeVideoTitleSpanParams{
		VideoID:   videoID,
		StartedAt: ts,
	}); err != nil {
		return fmt.Errorf("sqlite resume video title spans: %w", err)
	}
	if err := a.queries.ResumeVideoCategorySpan(ctx, sqlitegen.ResumeVideoCategorySpanParams{
		VideoID:   videoID,
		StartedAt: ts,
	}); err != nil {
		return fmt.Errorf("sqlite resume video category spans: %w", err)
	}
	return nil
}

// closeOpenVideoMetadataSpansWith runs both close queries against the
// supplied Queries handle. Separate from the SQLiteAdapter method so
// MarkVideoDone/MarkVideoFailed can pass their tx-scoped Queries and
// share atomicity with the terminal video update that follows.
func closeOpenVideoMetadataSpansWith(ctx context.Context, q *sqlitegen.Queries, videoID int64, at time.Time) error {
	ts := sqliteTime(at)
	if err := q.CloseOpenVideoTitleSpans(ctx, sqlitegen.CloseOpenVideoTitleSpansParams{
		AtTime:  &ts,
		VideoID: videoID,
	}); err != nil {
		return fmt.Errorf("sqlite close video title spans: %w", err)
	}
	if err := q.CloseOpenVideoCategorySpans(ctx, sqlitegen.CloseOpenVideoCategorySpansParams{
		AtTime:  &ts,
		VideoID: videoID,
	}); err != nil {
		return fmt.Errorf("sqlite close video category spans: %w", err)
	}
	return nil
}

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

func (a *SQLiteAdapter) ListVideosByJobIDs(ctx context.Context, jobIDs []string) ([]repository.Video, error) {
	if len(jobIDs) == 0 {
		return []repository.Video{}, nil
	}
	rows, err := a.queries.ListVideosByJobIDs(ctx, jobIDs)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by job ids: %w", err)
	}
	videos := make([]repository.Video, len(rows))
	for i, row := range rows {
		videos[i] = *sqliteVideoToDomain(row)
	}
	return videos, nil
}

func (a *SQLiteAdapter) CreateVideo(ctx context.Context, v *repository.VideoInput) (*repository.Video, error) {
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: v.RecordingType,
		Quality:       v.Quality,
		ForceH264:     v.ForceH264,
	})
	row, err := a.queries.CreateVideo(ctx, sqlitegen.CreateVideoParams{
		JobID:                     v.JobID,
		Filename:                  v.Filename,
		DisplayName:               v.DisplayName,
		Title:                     v.Title,
		Status:                    v.Status,
		Quality:                   settings.Quality,
		BroadcasterID:             v.BroadcasterID,
		StreamID:                  toNullString(v.StreamID),
		ViewerCount:               v.ViewerCount,
		Language:                  v.Language,
		RecordingType:             settings.RecordingType,
		ForceH264:                 boolToInt64(settings.ForceH264),
		TriggerScheduleID:         int64PtrToNullInt64(v.TriggerScheduleID),
		RetentionSourceScheduleID: int64PtrToNullInt64(v.RetentionSourceScheduleID),
		RetentionWindowHours:      int64PtrToNullInt64(v.RetentionWindowHours),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create video: %w", err)
	}
	return sqliteVideoToDomain(row), nil
}

func (a *SQLiteAdapter) UpdateVideoStatus(ctx context.Context, id int64, status string) error {
	return a.queries.UpdateVideoStatus(ctx, sqlitegen.UpdateVideoStatusParams{ID: id, Status: status})
}

func (a *SQLiteAdapter) UpdateVideoSelectedVariant(ctx context.Context, id int64, quality string, fps *float64) error {
	qualityNull := sql.NullString{String: quality, Valid: quality != ""}
	var fpsNull sql.NullFloat64
	if fps != nil {
		fpsNull = sql.NullFloat64{Float64: *fps, Valid: true}
	}
	return a.queries.UpdateVideoSelectedVariant(ctx, sqlitegen.UpdateVideoSelectedVariantParams{
		SelectedQuality: qualityNull,
		SelectedFps:     fpsNull,
		ID:              id,
	})
}

func (a *SQLiteAdapter) MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string, truncated bool) error {
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		return q.MarkVideoDone(ctx, sqlitegen.MarkVideoDoneParams{
			ID:              id,
			DurationSeconds: sql.NullFloat64{Float64: durationSeconds, Valid: true},
			SizeBytes:       sql.NullInt64{Int64: sizeBytes, Valid: true},
			Thumbnail:       toNullString(thumbnail),
			CompletionKind:  completionKind,
			Truncated:       sqliteBool(truncated),
		})
	})
}

func (a *SQLiteAdapter) MarkVideoDoneAndEnqueueRecordingWebhook(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string, truncated bool, delivery *repository.RecordingWebhookDeliveryInput) error {
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		if err := q.MarkVideoDone(ctx, sqlitegen.MarkVideoDoneParams{
			ID:              id,
			DurationSeconds: sql.NullFloat64{Float64: durationSeconds, Valid: true},
			SizeBytes:       sql.NullInt64{Int64: sizeBytes, Valid: true},
			Thumbnail:       toNullString(thumbnail),
			CompletionKind:  completionKind,
			Truncated:       sqliteBool(truncated),
		}); err != nil {
			return err
		}
		return sqliteCreateRecordingWebhookDeliveryIfEnabled(ctx, q, delivery)
	})
}

func (a *SQLiteAdapter) MarkVideoFailed(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool) error {
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		return q.MarkVideoFailed(ctx, sqlitegen.MarkVideoFailedParams{
			ID:             id,
			Error:          sql.NullString{String: errMsg, Valid: true},
			CompletionKind: completionKind,
			Truncated:      sqliteBool(truncated),
		})
	})
}

func (a *SQLiteAdapter) MarkVideoFailedAndEnqueueRecordingWebhook(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool, delivery *repository.RecordingWebhookDeliveryInput) error {
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		if err := q.MarkVideoFailed(ctx, sqlitegen.MarkVideoFailedParams{
			ID:             id,
			Error:          sql.NullString{String: errMsg, Valid: true},
			CompletionKind: completionKind,
			Truncated:      sqliteBool(truncated),
		}); err != nil {
			return err
		}
		return sqliteCreateRecordingWebhookDeliveryIfEnabled(ctx, q, delivery)
	})
}

// sqliteBool encodes a Go bool as the int64 0/1 SQLite uses for the
// truncated column. Pairs with the int64 → bool flatten in
// sqliteVideoToDomain so the adapter's interface stays plain bool.
func sqliteBool(v bool) int64 {
	if v {
		return 1
	}
	return 0
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
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind, truncated,
    trigger_schedule_id, retention_source_schedule_id, retention_window_hours
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

const listVideosByBroadcasterPageSQL = `SELECT
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind, truncated,
    trigger_schedule_id, retention_source_schedule_id, retention_window_hours
FROM videos
WHERE broadcaster_id = ?1
  AND deleted_at IS NULL
  AND (
    ?2 IS NULL
    OR start_download_at < ?2
    OR (start_download_at = ?2 AND id < ?3)
  )
ORDER BY start_download_at DESC, id DESC
LIMIT ?4`

const listVideosByCategoryPageSQL = `SELECT
    v.id, v.job_id, v.filename, v.display_name, v.status, v.quality, v.selected_quality,
    v.selected_fps, v.broadcaster_id, v.stream_id, v.viewer_count, v.language,
    v.duration_seconds, v.size_bytes, v.thumbnail, v.error,
    v.start_download_at, v.downloaded_at, v.deleted_at,
    v.recording_type, v.force_h264, v.title, v.completion_kind, v.truncated,
    v.trigger_schedule_id, v.retention_source_schedule_id, v.retention_window_hours
FROM videos v
INNER JOIN video_categories vc ON vc.video_id = v.id
WHERE vc.category_id = ?1
  AND v.deleted_at IS NULL
  AND (
    ?2 IS NULL
    OR v.start_download_at < ?2
    OR (v.start_download_at = ?2 AND v.id < ?3)
  )
ORDER BY v.start_download_at DESC, v.id DESC
LIMIT ?4`

const searchVideosSQL = `WITH q AS (
    SELECT
        lower(?1) AS term,
        lower(?1) || '%' AS prefix,
        '%' || lower(?1) || '%' AS contains
),
title_matches AS (
    SELECT
        vt.video_id,
        MAX(lower(t.name) = q.term) AS title_exact,
        MAX(lower(t.name) LIKE q.prefix) AS title_prefix,
        MAX(lower(t.name) LIKE q.contains) AS title_contains
    FROM video_titles vt
    INNER JOIN titles t ON t.id = vt.title_id
    CROSS JOIN q
    GROUP BY vt.video_id
),
category_matches AS (
    SELECT
        vc.video_id,
        MAX(lower(c.name) = q.term) AS category_exact,
        MAX(lower(c.name) LIKE q.prefix) AS category_prefix,
        MAX(lower(c.name) LIKE q.contains) AS category_contains
    FROM video_categories vc
    INNER JOIN categories c ON c.id = vc.category_id
    CROSS JOIN q
    GROUP BY vc.video_id
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
        lower(coalesce(v.title, '')) = q.term OR coalesce(tm.title_exact, 0) AS title_exact,
        lower(coalesce(v.title, '')) LIKE q.prefix OR coalesce(tm.title_prefix, 0) AS title_prefix,
        lower(coalesce(v.title, '')) LIKE q.contains OR coalesce(tm.title_contains, 0) AS title_contains,
        lower(coalesce(v.display_name, '')) = q.term
            OR lower(coalesce(ch.broadcaster_login, '')) = q.term
            OR lower(coalesce(ch.broadcaster_name, '')) = q.term AS channel_exact,
        lower(coalesce(v.display_name, '')) LIKE q.prefix
            OR lower(coalesce(ch.broadcaster_login, '')) LIKE q.prefix
            OR lower(coalesce(ch.broadcaster_name, '')) LIKE q.prefix AS channel_prefix,
        lower(coalesce(v.display_name, '')) LIKE q.contains
            OR lower(coalesce(ch.broadcaster_login, '')) LIKE q.contains
            OR lower(coalesce(ch.broadcaster_name, '')) LIKE q.contains AS channel_contains,
        coalesce(cm.category_exact, 0) AS category_exact,
        coalesce(cm.category_prefix, 0) AS category_prefix,
        coalesce(cm.category_contains, 0) AS category_contains
    FROM videos v
    CROSS JOIN q
    LEFT JOIN channels ch ON ch.broadcaster_id = v.broadcaster_id
    LEFT JOIN title_matches tm ON tm.video_id = v.id
    LEFT JOIN category_matches cm ON cm.video_id = v.id
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
LIMIT ?2`

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
			return nil, fmt.Errorf("sqlite list videos scan: %w", err)
		}
		out = append(out, *sqliteVideoToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite list videos: %w", err)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListVideosPage(ctx context.Context, opts repository.ListVideosOpts, cursor *repository.VideoListPageCursor) (*repository.VideoListPage, error) {
	query, args := repository.BuildListVideosPageQuery(opts, cursor, repository.VideoPageDialect{
		Postgres:   false,
		FormatTime: func(t time.Time) any { return sqliteTime(t) },
	})
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos page: %w", err)
	}
	items, err := scanSQLiteVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos page: %w", err)
	}
	return repository.ToVideoListPage(items, opts), nil
}

func (a *SQLiteAdapter) SearchVideos(ctx context.Context, query string, limit int) ([]repository.Video, error) {
	rows, err := a.db.QueryContext(ctx, searchVideosSQL, query, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("sqlite search videos: %w", err)
	}
	items, err := scanSQLiteVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite search videos: %w", err)
	}
	return items, nil
}

func (a *SQLiteAdapter) ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.db.QueryContext(ctx, listVideosByBroadcasterPageSQL,
		broadcasterID,
		sqliteCursorStartDownloadAt(cursor),
		sqliteCursorID(cursor),
		int64(limit+1),
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by broadcaster: %w", err)
	}
	items, err := scanSQLiteVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by broadcaster: %w", err)
	}
	return repository.ToVideoPage(items, limit), nil
}

func (a *SQLiteAdapter) ListVideosByCategory(ctx context.Context, categoryID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.db.QueryContext(ctx, listVideosByCategoryPageSQL,
		categoryID,
		sqliteCursorStartDownloadAt(cursor),
		sqliteCursorID(cursor),
		int64(limit+1),
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by category: %w", err)
	}
	items, err := scanSQLiteVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by category: %w", err)
	}
	return repository.ToVideoPage(items, limit), nil
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

func (a *SQLiteAdapter) ListFinishedVideosForRetention(ctx context.Context, now time.Time) ([]repository.RetentionVideo, error) {
	rows, err := a.queries.ListFinishedVideosForRetention(ctx, sqliteTimePtr(&now))
	if err != nil {
		return nil, fmt.Errorf("sqlite list finished videos for retention: %w", err)
	}
	out := make([]repository.RetentionVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.RetentionVideo{
			VideoID:              r.ID,
			BroadcasterID:        r.BroadcasterID,
			DownloadedAt:         timePtrFromSQLite(r.DownloadedAt),
			RetentionWindowHours: nullInt64ToInt64Ptr(r.RetentionWindowHours),
		}
	}
	return out, nil
}

func (a *SQLiteAdapter) FinalizeRetentionDelete(ctx context.Context, videoID int64) error {
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := q.SoftDeleteVideo(ctx, videoID); err != nil {
			return fmt.Errorf("sqlite retention tombstone video: %w", err)
		}
		if err := q.DeleteVideoParts(ctx, videoID); err != nil {
			return fmt.Errorf("sqlite retention delete parts: %w", err)
		}
		return nil
	})
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

// VideoStatsTotals issues four atomic aggregate queries and combines
// them. The PG path uses one SELECT with FILTER clauses; sqlc's
// SQLite engine miscompiles that shape (truncates the const string
// and bleeds chars into adjacent queries), so the SQLite side is
// hand-composed from queries that codegen cleanly.
func (a *SQLiteAdapter) VideoStatsTotals(ctx context.Context) (*repository.VideoStatsTotals, error) {
	doneRow, err := a.queries.StatisticsTotalsDoneOnly(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals (done): %w", err)
	}
	thisWeek, err := a.queries.StatisticsThisWeek(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals (this_week): %w", err)
	}
	incomplete, err := a.queries.StatisticsIncomplete(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals (incomplete): %w", err)
	}
	channels, err := a.queries.StatisticsChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals (channels): %w", err)
	}
	return &repository.VideoStatsTotals{
		Total:         doneRow.Total,
		TotalSize:     doneRow.TotalSize,
		TotalDuration: doneRow.TotalDuration,
		ThisWeek:      thisWeek,
		Incomplete:    incomplete,
		Channels:      channels,
	}, nil
}

func (a *SQLiteAdapter) VideoStatsTotalsByBroadcaster(ctx context.Context, broadcasterID string) (*repository.VideoStatsTotals, error) {
	row, err := a.queries.StatisticsTotalsByBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals by broadcaster: %w", err)
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
	var selectedFPS *float64
	if v.SelectedFps.Valid {
		f := v.SelectedFps.Float64
		selectedFPS = &f
	}
	return &repository.Video{
		ID:                        v.ID,
		JobID:                     v.JobID,
		Filename:                  v.Filename,
		DisplayName:               v.DisplayName,
		Title:                     v.Title,
		Status:                    v.Status,
		Quality:                   v.Quality,
		SelectedQuality:           fromNullString(v.SelectedQuality),
		SelectedFPS:               selectedFPS,
		BroadcasterID:             v.BroadcasterID,
		StreamID:                  fromNullString(v.StreamID),
		ViewerCount:               v.ViewerCount,
		Language:                  v.Language,
		DurationSeconds:           duration,
		SizeBytes:                 size,
		Thumbnail:                 fromNullString(v.Thumbnail),
		Error:                     fromNullString(v.Error),
		StartDownloadAt:           v.StartDownloadAt.Time,
		DownloadedAt:              timePtrFromSQLite(v.DownloadedAt),
		DeletedAt:                 timePtrFromSQLite(v.DeletedAt),
		RecordingType:             v.RecordingType,
		ForceH264:                 v.ForceH264 != 0,
		TriggerScheduleID:         nullInt64ToInt64Ptr(v.TriggerScheduleID),
		RetentionSourceScheduleID: nullInt64ToInt64Ptr(v.RetentionSourceScheduleID),
		RetentionWindowHours:      nullInt64ToInt64Ptr(v.RetentionWindowHours),
		CompletionKind:            v.CompletionKind,
		Truncated:                 v.Truncated != 0,
	}
}

func sqliteVideosToDomain(rows []sqlitegen.Video) []repository.Video {
	out := make([]repository.Video, len(rows))
	for i, r := range rows {
		out[i] = *sqliteVideoToDomain(r)
	}
	return out
}

func scanSQLiteVideos(rows *sql.Rows) ([]repository.Video, error) {
	defer rows.Close()
	out := []repository.Video{}
	for rows.Next() {
		row, err := scanSQLiteVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sqliteVideoToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanSQLiteVideo(rows *sql.Rows) (sqlitegen.Video, error) {
	var row sqlitegen.Video
	err := rows.Scan(
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
	)
	return row, err
}

func sqliteCursorStartDownloadAt(cursor *repository.VideoPageCursor) any {
	if cursor == nil {
		return nil
	}
	return sqliteTime(cursor.StartDownloadAt)
}

func sqliteCursorID(cursor *repository.VideoPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.ID
}

// Pure page/cursor helpers now live in repository (pagination.go) so both
// adapters share one copy.
