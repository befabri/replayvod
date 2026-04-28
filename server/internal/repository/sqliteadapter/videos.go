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
	ts := formatTime(at.UTC())
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
	ts := sql.NullString{String: formatTime(at.UTC()), Valid: true}
	if err := q.CloseOpenVideoTitleSpans(ctx, sqlitegen.CloseOpenVideoTitleSpansParams{
		AtTime:  ts,
		VideoID: videoID,
	}); err != nil {
		return fmt.Errorf("sqlite close video title spans: %w", err)
	}
	if err := q.CloseOpenVideoCategorySpans(ctx, sqlitegen.CloseOpenVideoCategorySpansParams{
		AtTime:  ts,
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
    recording_type, force_h264, title, completion_kind, truncated
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
    recording_type, force_h264, title, completion_kind, truncated
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
    v.recording_type, v.force_h264, v.title, v.completion_kind, v.truncated
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

const listVideosPageBaseSQL = `SELECT
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind, truncated
FROM videos
WHERE deleted_at IS NULL
  AND (? = '' OR status = ?)`

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
	query, args := sqliteListVideosPageQueryAndArgs(opts, cursor)
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos page: %w", err)
	}
	items, err := scanSQLiteVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos page: %w", err)
	}
	return toVideoListPage(items, opts), nil
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
	return toVideoPage(items, limit), nil
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
	return toVideoPage(items, limit), nil
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
		ID:              v.ID,
		JobID:           v.JobID,
		Filename:        v.Filename,
		DisplayName:     v.DisplayName,
		Title:           v.Title,
		Status:          v.Status,
		Quality:         v.Quality,
		SelectedQuality: fromNullString(v.SelectedQuality),
		SelectedFPS:     selectedFPS,
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
		Truncated:       v.Truncated != 0,
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
	)
	return row, err
}

func sqliteCursorStartDownloadAt(cursor *repository.VideoPageCursor) any {
	if cursor == nil {
		return nil
	}
	return formatTime(cursor.StartDownloadAt.UTC())
}

func sqliteCursorID(cursor *repository.VideoPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.ID
}

func sqliteListVideosPageQueryAndArgs(opts repository.ListVideosOpts, cursor *repository.VideoListPageCursor) (string, []any) {
	sort, order := normalizeVideoListSort(opts)
	limit := int64(listVideosPageQueryLimit(opts.Limit))
	args := []any{opts.Status, opts.Status}
	query := listVideosPageBaseSQL + sqliteVideoListFiltersSQL(&args, opts)
	appendLimit := func() { args = append(args, limit) }
	switch sort + ":" + order {
	case "created_at:asc":
		t := sqliteVideoListCursorTime(cursor)
		args = append(args, t, t, t, sqliteVideoListCursorID(cursor))
		query += `
  AND (? IS NULL OR start_download_at > ? OR (start_download_at = ? AND id > ?))
ORDER BY start_download_at ASC, id ASC
LIMIT ?`
	case "channel:asc":
		text := sqliteVideoListCursorText(cursor)
		t := sqliteVideoListCursorTime(cursor)
		args = append(args, text, text, text, t, t, sqliteVideoListCursorID(cursor))
		query += `
  AND (? IS NULL OR display_name > ?
    OR (display_name = ? AND (start_download_at < ? OR (start_download_at = ? AND id > ?))))
ORDER BY display_name ASC, start_download_at DESC, id ASC
LIMIT ?`
	case "channel:desc":
		text := sqliteVideoListCursorText(cursor)
		t := sqliteVideoListCursorTime(cursor)
		args = append(args, text, text, text, t, t, sqliteVideoListCursorID(cursor))
		query += `
  AND (? IS NULL OR display_name < ?
    OR (display_name = ? AND (start_download_at < ? OR (start_download_at = ? AND id < ?))))
ORDER BY display_name DESC, start_download_at DESC, id DESC
LIMIT ?`
	case "duration:asc":
		n := sqliteVideoListCursorNumber(cursor)
		t := sqliteVideoListCursorTime(cursor)
		id := sqliteVideoListCursorID(cursor)
		args = append(args, t, n, t, t, id, n, n, n, t, t, id)
		query += `
  AND (
    ? IS NULL
    OR (? IS NULL AND duration_seconds IS NULL AND (start_download_at < ? OR (start_download_at = ? AND id > ?)))
    OR (? IS NOT NULL AND (duration_seconds IS NULL OR duration_seconds > ? OR (duration_seconds = ? AND (start_download_at < ? OR (start_download_at = ? AND id > ?)))))
  )
ORDER BY duration_seconds ASC NULLS LAST, start_download_at DESC, id ASC
LIMIT ?`
	case "duration:desc":
		n := sqliteVideoListCursorNumber(cursor)
		t := sqliteVideoListCursorTime(cursor)
		id := sqliteVideoListCursorID(cursor)
		args = append(args, t, n, t, t, id, n, n, n, t, t, id)
		query += `
  AND (
    ? IS NULL
    OR (? IS NULL AND duration_seconds IS NULL AND (start_download_at < ? OR (start_download_at = ? AND id < ?)))
    OR (? IS NOT NULL AND (duration_seconds IS NULL OR duration_seconds < ? OR (duration_seconds = ? AND (start_download_at < ? OR (start_download_at = ? AND id < ?)))))
  )
ORDER BY duration_seconds DESC NULLS LAST, start_download_at DESC, id DESC
LIMIT ?`
	case "size:asc":
		n := sqliteVideoListCursorInt(cursor)
		t := sqliteVideoListCursorTime(cursor)
		id := sqliteVideoListCursorID(cursor)
		args = append(args, t, n, t, t, id, n, n, n, t, t, id)
		query += `
  AND (
    ? IS NULL
    OR (? IS NULL AND size_bytes IS NULL AND (start_download_at < ? OR (start_download_at = ? AND id > ?)))
    OR (? IS NOT NULL AND (size_bytes IS NULL OR size_bytes > ? OR (size_bytes = ? AND (start_download_at < ? OR (start_download_at = ? AND id > ?)))))
  )
ORDER BY size_bytes ASC NULLS LAST, start_download_at DESC, id ASC
LIMIT ?`
	case "size:desc":
		n := sqliteVideoListCursorInt(cursor)
		t := sqliteVideoListCursorTime(cursor)
		id := sqliteVideoListCursorID(cursor)
		args = append(args, t, n, t, t, id, n, n, n, t, t, id)
		query += `
  AND (
    ? IS NULL
    OR (? IS NULL AND size_bytes IS NULL AND (start_download_at < ? OR (start_download_at = ? AND id < ?)))
    OR (? IS NOT NULL AND (size_bytes IS NULL OR size_bytes < ? OR (size_bytes = ? AND (start_download_at < ? OR (start_download_at = ? AND id < ?)))))
  )
ORDER BY size_bytes DESC NULLS LAST, start_download_at DESC, id DESC
LIMIT ?`
	default:
		t := sqliteVideoListCursorTime(cursor)
		args = append(args, t, t, t, sqliteVideoListCursorID(cursor))
		query += `
  AND (? IS NULL OR start_download_at < ? OR (start_download_at = ? AND id < ?))
ORDER BY start_download_at DESC, id DESC
LIMIT ?`
	}
	appendLimit()
	return query, args
}

func sqliteVideoListFiltersSQL(args *[]any, opts repository.ListVideosOpts) string {
	quality := opts.Quality
	*args = append(*args, quality, quality, quality, quality, quality)
	*args = append(*args, opts.BroadcasterID, opts.BroadcasterID)
	*args = append(*args, opts.Language, opts.Language)
	durationMin := sqliteOptionalFloat(opts.DurationMinSeconds)
	durationMax := sqliteOptionalFloat(opts.DurationMaxSeconds)
	sizeMin := sqliteOptionalInt(opts.SizeMinBytes)
	sizeMax := sqliteOptionalInt(opts.SizeMaxBytes)
	*args = append(*args, durationMin, durationMin)
	*args = append(*args, durationMax, durationMax)
	*args = append(*args, sizeMin, sizeMin)
	*args = append(*args, sizeMax, sizeMax)
	*args = append(*args, opts.Window, opts.Window)
	*args = append(*args, sqliteBool(opts.IncompleteOnly))
	return `
  AND (? = '' OR quality = ? OR selected_quality = ? OR selected_quality || 'p' = ? OR (selected_fps IS NOT NULL AND selected_fps > 0 AND selected_quality || 'p' || CAST(ROUND(selected_fps) AS INTEGER) = ?))
  AND (? = '' OR broadcaster_id = ?)
  AND (? = '' OR language = ?)
  AND (? IS NULL OR duration_seconds >= ?)
  AND (? IS NULL OR duration_seconds < ?)
  AND (? IS NULL OR size_bytes >= ?)
  AND (? IS NULL OR size_bytes < ?)
  AND (? = '' OR (? = 'this_week' AND start_download_at >= datetime('now', '-7 days')))
  AND (? = 0 OR completion_kind = 'partial' OR truncated = 1)`
}

func sqliteOptionalFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func sqliteOptionalInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func sqliteVideoListCursorTime(cursor *repository.VideoListPageCursor) any {
	if cursor == nil {
		return nil
	}
	return formatTime(cursor.StartDownloadAt.UTC())
}

func sqliteVideoListCursorID(cursor *repository.VideoListPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.ID
}

func sqliteVideoListCursorText(cursor *repository.VideoListPageCursor) any {
	if cursor == nil || cursor.SortText == nil {
		return nil
	}
	return *cursor.SortText
}

func sqliteVideoListCursorNumber(cursor *repository.VideoListPageCursor) any {
	if cursor == nil || cursor.SortNumber == nil {
		return nil
	}
	return *cursor.SortNumber
}

func sqliteVideoListCursorInt(cursor *repository.VideoListPageCursor) any {
	if cursor == nil || cursor.SortInt == nil {
		return nil
	}
	return *cursor.SortInt
}

func toVideoListPage(items []repository.Video, opts repository.ListVideosOpts) *repository.VideoListPage {
	if opts.Limit <= 0 {
		return &repository.VideoListPage{Items: []repository.Video{}}
	}
	page := &repository.VideoListPage{Items: items}
	if len(items) <= opts.Limit {
		return page
	}
	page.Items = items[:opts.Limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor = videoListCursorFromVideo(&last, opts)
	return page
}

func normalizeVideoListSort(opts repository.ListVideosOpts) (string, string) {
	sort := opts.Sort
	order := opts.Order
	switch sort {
	case "created_at", "duration", "size", "channel":
	default:
		return "created_at", "desc"
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	return sort, order
}

func listVideosPageQueryLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	return limit + 1
}

func videoListCursorFromVideo(v *repository.Video, opts repository.ListVideosOpts) *repository.VideoListPageCursor {
	if v == nil {
		return nil
	}
	cursor := &repository.VideoListPageCursor{StartDownloadAt: v.StartDownloadAt, ID: v.ID}
	sort, _ := normalizeVideoListSort(opts)
	switch sort {
	case "duration":
		cursor.SortNumber = v.DurationSeconds
	case "size":
		cursor.SortInt = v.SizeBytes
	case "channel":
		cursor.SortText = &v.DisplayName
	}
	return cursor
}

func toVideoPage(items []repository.Video, limit int) *repository.VideoPage {
	if limit <= 0 {
		return &repository.VideoPage{Items: []repository.Video{}}
	}
	page := &repository.VideoPage{Items: items}
	if len(items) <= limit {
		return page
	}
	page.Items = items[:limit]
	next := page.Items[len(page.Items)-1]
	page.NextCursor = &repository.VideoPageCursor{StartDownloadAt: next.StartDownloadAt, ID: next.ID}
	return page
}
