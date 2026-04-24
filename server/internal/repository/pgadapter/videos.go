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
    recording_type, force_h264, title, completion_kind
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
    v.recording_type, v.force_h264, v.title, v.completion_kind
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

const listVideosPageBaseSQL = `SELECT
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind
FROM videos
WHERE deleted_at IS NULL
  AND ($1::text = '' OR status = $1::text)`

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

func (a *PGAdapter) CreateVideo(ctx context.Context, v *repository.VideoInput) (*repository.Video, error) {
	rt := v.RecordingType
	if rt == "" {
		rt = repository.RecordingTypeVideo
	}
	row, err := a.queries.CreateVideo(ctx, pggen.CreateVideoParams{
		JobID:         v.JobID,
		Filename:      v.Filename,
		DisplayName:   v.DisplayName,
		Title:         v.Title,
		Status:        v.Status,
		Quality:       v.Quality,
		BroadcasterID: v.BroadcasterID,
		StreamID:      v.StreamID,
		ViewerCount:   int32(v.ViewerCount),
		Language:      v.Language,
		RecordingType: rt,
		ForceH264:     v.ForceH264,
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

func (a *PGAdapter) MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string) error {
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
		})
	})
}

func (a *PGAdapter) MarkVideoFailed(ctx context.Context, id int64, errMsg string, completionKind string) error {
	return a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		if err := closeOpenVideoMetadataSpansWith(ctx, q, id, time.Now().UTC()); err != nil {
			return err
		}
		return q.MarkVideoFailed(ctx, pggen.MarkVideoFailedParams{
			ID:             id,
			Error:          &errMsg,
			CompletionKind: completionKind,
		})
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
	query, args := pgListVideosPageQueryAndArgs(opts, cursor)
	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("pg list videos page: %w", err)
	}
	items, err := scanPGVideos(rows)
	if err != nil {
		return nil, fmt.Errorf("pg list videos page: %w", err)
	}
	return toVideoListPage(items, opts), nil
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
	return toVideoPage(items, limit), nil
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
	return toVideoPage(items, limit), nil
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
		Title:           v.Title,
		Status:          v.Status,
		Quality:         v.Quality,
		SelectedQuality: v.SelectedQuality,
		SelectedFPS:     v.SelectedFps,
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
		RecordingType:   v.RecordingType,
		ForceH264:       v.ForceH264,
		CompletionKind:  v.CompletionKind,
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

func pgListVideosPageQueryAndArgs(opts repository.ListVideosOpts, cursor *repository.VideoListPageCursor) (string, []any) {
	sort, order := normalizeVideoListSort(opts)
	limit := listVideosPageQueryLimit(opts.Limit)
	args := []any{opts.Status}
	query := listVideosPageBaseSQL + pgVideoListFiltersSQL(&args, opts)
	appendLimit := func() string { return pgAppendArg(&args, limit) }
	switch sort + ":" + order {
	case "created_at:asc":
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (%s::timestamptz IS NULL OR start_download_at > %s::timestamptz OR (start_download_at = %s::timestamptz AND id > %s))
ORDER BY start_download_at ASC, id ASC
LIMIT %s`, t, t, t, id, appendLimit())
	case "channel:asc":
		text := pgAppendArg(&args, pgVideoListCursorText(cursor))
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (%s::text IS NULL
    OR display_name > %s::text
    OR (display_name = %s::text
      AND (start_download_at < %s::timestamptz
        OR (start_download_at = %s::timestamptz AND id > %s))))
ORDER BY display_name ASC, start_download_at DESC, id ASC
LIMIT %s`, text, text, text, t, t, id, appendLimit())
	case "channel:desc":
		text := pgAppendArg(&args, pgVideoListCursorText(cursor))
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (%s::text IS NULL
    OR display_name < %s::text
    OR (display_name = %s::text
      AND (start_download_at < %s::timestamptz
        OR (start_download_at = %s::timestamptz AND id < %s))))
ORDER BY display_name DESC, start_download_at DESC, id DESC
LIMIT %s`, text, text, text, t, t, id, appendLimit())
	case "duration:asc":
		n := pgAppendArg(&args, pgVideoListCursorNumber(cursor))
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (
    %s::timestamptz IS NULL
    OR %s::double precision IS NULL
      AND duration_seconds IS NULL
      AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id > %s))
    OR %s::double precision IS NOT NULL
      AND (duration_seconds IS NULL
        OR duration_seconds > %s::double precision
        OR (duration_seconds = %s::double precision
          AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id > %s))))
  )
ORDER BY duration_seconds ASC NULLS LAST, start_download_at DESC, id ASC
LIMIT %s`, t, n, t, t, id, n, n, n, t, t, id, appendLimit())
	case "duration:desc":
		n := pgAppendArg(&args, pgVideoListCursorNumber(cursor))
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (
    %s::timestamptz IS NULL
    OR %s::double precision IS NULL
      AND duration_seconds IS NULL
      AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id < %s))
    OR %s::double precision IS NOT NULL
      AND (duration_seconds IS NULL
        OR duration_seconds < %s::double precision
        OR (duration_seconds = %s::double precision
          AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id < %s))))
  )
ORDER BY duration_seconds DESC NULLS LAST, start_download_at DESC, id DESC
LIMIT %s`, t, n, t, t, id, n, n, n, t, t, id, appendLimit())
	case "size:asc":
		n := pgAppendArg(&args, pgVideoListCursorInt(cursor))
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (
    %s::timestamptz IS NULL
    OR %s::bigint IS NULL
      AND size_bytes IS NULL
      AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id > %s))
    OR %s::bigint IS NOT NULL
      AND (size_bytes IS NULL
        OR size_bytes > %s::bigint
        OR (size_bytes = %s::bigint
          AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id > %s))))
  )
ORDER BY size_bytes ASC NULLS LAST, start_download_at DESC, id ASC
LIMIT %s`, t, n, t, t, id, n, n, n, t, t, id, appendLimit())
	case "size:desc":
		n := pgAppendArg(&args, pgVideoListCursorInt(cursor))
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (
    %s::timestamptz IS NULL
    OR %s::bigint IS NULL
      AND size_bytes IS NULL
      AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id < %s))
    OR %s::bigint IS NOT NULL
      AND (size_bytes IS NULL
        OR size_bytes < %s::bigint
        OR (size_bytes = %s::bigint
          AND (start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id < %s))))
  )
ORDER BY size_bytes DESC NULLS LAST, start_download_at DESC, id DESC
LIMIT %s`, t, n, t, t, id, n, n, n, t, t, id, appendLimit())
	default:
		t := pgAppendArg(&args, pgVideoListCursorTime(cursor))
		id := pgAppendArg(&args, pgVideoListCursorID(cursor))
		query += fmt.Sprintf(`
  AND (%s::timestamptz IS NULL OR start_download_at < %s::timestamptz OR (start_download_at = %s::timestamptz AND id < %s))
ORDER BY start_download_at DESC, id DESC
LIMIT %s`, t, t, t, id, appendLimit())
	}
	return query, args
}

func pgVideoListFiltersSQL(args *[]any, opts repository.ListVideosOpts) string {
	quality := pgAppendArg(args, opts.Quality)
	broadcasterID := pgAppendArg(args, opts.BroadcasterID)
	language := pgAppendArg(args, opts.Language)
	durationMin := pgAppendArg(args, opts.DurationMinSeconds)
	durationMax := pgAppendArg(args, opts.DurationMaxSeconds)
	sizeMin := pgAppendArg(args, opts.SizeMinBytes)
	sizeMax := pgAppendArg(args, opts.SizeMaxBytes)
	return fmt.Sprintf(`
  AND (%s::text = '' OR quality = %s::text OR selected_quality = %s::text OR selected_quality || 'p' = %s::text OR (selected_fps IS NOT NULL AND selected_fps > 0 AND selected_quality || 'p' || ROUND(selected_fps)::int::text = %s::text))
  AND (%s::text = '' OR broadcaster_id = %s::text)
  AND (%s::text = '' OR language = %s::text)
  AND (%s::double precision IS NULL OR duration_seconds >= %s::double precision)
  AND (%s::double precision IS NULL OR duration_seconds < %s::double precision)
  AND (%s::bigint IS NULL OR size_bytes >= %s::bigint)
  AND (%s::bigint IS NULL OR size_bytes < %s::bigint)`, quality, quality, quality, quality, quality, broadcasterID, broadcasterID, language, language, durationMin, durationMin, durationMax, durationMax, sizeMin, sizeMin, sizeMax, sizeMax)
}

func pgAppendArg(args *[]any, value any) string {
	*args = append(*args, value)
	return fmt.Sprintf("$%d", len(*args))
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

func pgVideoListCursorTime(cursor *repository.VideoListPageCursor) *time.Time {
	if cursor == nil {
		return nil
	}
	t := cursor.StartDownloadAt.UTC()
	return &t
}

func pgVideoListCursorID(cursor *repository.VideoListPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.ID
}

func pgVideoListCursorText(cursor *repository.VideoListPageCursor) *string {
	if cursor == nil {
		return nil
	}
	return cursor.SortText
}

func pgVideoListCursorNumber(cursor *repository.VideoListPageCursor) *float64 {
	if cursor == nil {
		return nil
	}
	return cursor.SortNumber
}

func pgVideoListCursorInt(cursor *repository.VideoListPageCursor) *int64 {
	if cursor == nil {
		return nil
	}
	return cursor.SortInt
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
