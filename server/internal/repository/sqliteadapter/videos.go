package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitetype"
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
		TriggerScheduleID:         toNullInt64(v.TriggerScheduleID),
		RetentionSourceScheduleID: toNullInt64(v.RetentionSourceScheduleID),
		RetentionWindowHours:      toNullInt64(v.RetentionWindowHours),
		Source:                    repository.VideoSourceOrLive(v.Source),
		TwitchVideoID:             toNullString(v.TwitchVideoID),
		BroadcastAt:               sqliteTimePtr(v.BroadcastAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create video: %w", mapErr(err))
	}
	return sqliteVideoToDomain(row), nil
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

func (a *SQLiteAdapter) ListVideos(ctx context.Context, opts repository.ListVideosOpts) ([]repository.Video, error) {
	rows, err := a.queries.ListVideos(ctx, sqlitegen.ListVideosParams{
		StatusFilter: opts.Status,
		SortKey:      opts.SortKey(),
		RowLimit:     int64(opts.Limit),
		RowOffset:    int64(opts.Offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
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
	rows, err := a.queries.SearchVideos(ctx, sqlitegen.SearchVideosParams{
		Query:    query,
		RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite search videos: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.queries.ListVideosByBroadcasterPage(ctx, sqlitegen.ListVideosByBroadcasterPageParams{
		BroadcasterID:         broadcasterID,
		CursorStartDownloadAt: sqliteCursorStartDownloadAt(cursor),
		CursorID:              sqliteCursorID(cursor),
		RowLimit:              int64(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by broadcaster: %w", err)
	}
	items := sqliteVideosToDomain(rows)
	return repository.ToVideoPage(items, limit), nil
}

func (a *SQLiteAdapter) ListVideosByCategory(ctx context.Context, categoryID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.queries.ListVideosByCategoryPage(ctx, sqlitegen.ListVideosByCategoryPageParams{
		CategoryID:            categoryID,
		CursorStartDownloadAt: sqliteCursorStartDownloadAt(cursor),
		CursorID:              sqliteCursorID(cursor),
		RowLimit:              int64(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos by category: %w", err)
	}
	items := sqliteVideosToDomain(rows)
	return repository.ToVideoPage(items, limit), nil
}

func (a *SQLiteAdapter) RequestVideoDelete(ctx context.Context, id int64) (*repository.Video, error) {
	row, err := a.queries.RequestVideoDelete(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoToDomain(row), nil
}

func (a *SQLiteAdapter) ListVideosPendingManualDelete(ctx context.Context, afterID int64, limit int) ([]repository.Video, error) {
	if limit <= 0 {
		return []repository.Video{}, nil
	}
	rows, err := a.queries.ListVideosPendingManualDelete(ctx, sqlitegen.ListVideosPendingManualDeleteParams{AfterID: afterID, RowLimit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos pending manual delete: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) SoftDeleteVideo(ctx context.Context, id int64, kind string) error {
	return a.queries.SoftDeleteVideo(ctx, sqlitegen.SoftDeleteVideoParams{
		ID:           id,
		DeletionKind: sql.NullString{String: kind, Valid: true},
	})
}

func (a *SQLiteAdapter) ListRetentionCandidates(ctx context.Context, now time.Time, afterID int64, limit int) ([]repository.RetentionVideo, error) {
	rows, err := a.queries.ListRetentionCandidates(ctx, sqlitegen.ListRetentionCandidatesParams{Now: sqliteTimePtr(&now), AfterID: afterID, BatchLimit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list finished videos for retention: %w", err)
	}
	out := make([]repository.RetentionVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.RetentionVideo{
			VideoID:              r.ID,
			BroadcasterID:        r.BroadcasterID,
			DownloadedAt:         timePtrFromSQLite(r.DownloadedAt),
			RetentionWindowHours: fromNullInt64(r.RetentionWindowHours),
		}
	}
	return out, nil
}

func (a *SQLiteAdapter) FinalizeDelete(ctx context.Context, videoID int64, kind string) error {
	return a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		if err := q.SoftDeleteVideo(ctx, sqlitegen.SoftDeleteVideoParams{
			ID:           videoID,
			DeletionKind: sql.NullString{String: kind, Valid: true},
		}); err != nil {
			return fmt.Errorf("sqlite tombstone video: %w", err)
		}
		if err := q.DeleteVideoParts(ctx, videoID); err != nil {
			return fmt.Errorf("sqlite delete parts: %w", err)
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

func (a *SQLiteAdapter) VideoStatsHistory(ctx context.Context) ([]repository.VideoStatsHistoryBucket, error) {
	rows, err := a.queries.StatisticsHistory(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats history: %w", err)
	}
	out := make([]repository.VideoStatsHistoryBucket, len(rows))
	for i, r := range rows {
		out[i] = repository.VideoStatsHistoryBucket{
			Status:         r.Status,
			CompletionKind: r.CompletionKind,
			Removed:        r.Removed != 0,
			DeletionKind:   r.DeletionKind,
			Count:          r.Count,
		}
	}
	return out, nil
}

// VideoStatsTotals issues four atomic aggregate queries and combines
// them. The PG path uses one SELECT with FILTER clauses; sqlc's
// SQLite engine miscompiles that shape (truncates the const string
// and bleeds chars into adjacent queries), so the SQLite side is
// hand-composed from queries that codegen cleanly.
func (a *SQLiteAdapter) VideoStatsTotals(ctx context.Context, userID string) (*repository.VideoStatsTotals, error) {
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
	removed, err := a.queries.StatisticsRemoved(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals (removed): %w", err)
	}
	watchLater, err := a.queries.StatisticsWatchLater(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals (watch_later): %w", err)
	}
	unwatched, err := a.queries.StatisticsUnwatched(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite video stats totals (unwatched): %w", err)
	}
	return &repository.VideoStatsTotals{
		Total:         doneRow.Total,
		TotalSize:     doneRow.TotalSize,
		TotalDuration: doneRow.TotalDuration,
		ThisWeek:      thisWeek,
		Incomplete:    incomplete,
		Channels:      channels,
		Removed:       removed,
		WatchLater:    watchLater,
		Unwatched:     unwatched,
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
		DeleteRequestedAt:         timePtrFromSQLite(v.DeleteRequestedAt),
		DeletionKind:              fromNullString(v.DeletionKind),
		RecordingType:             v.RecordingType,
		ForceH264:                 v.ForceH264 != 0,
		TriggerScheduleID:         fromNullInt64(v.TriggerScheduleID),
		RetentionSourceScheduleID: fromNullInt64(v.RetentionSourceScheduleID),
		RetentionWindowHours:      fromNullInt64(v.RetentionWindowHours),
		CompletionKind:            v.CompletionKind,
		Truncated:                 v.Truncated != 0,
		Source:                    v.Source,
		TwitchVideoID:             fromNullString(v.TwitchVideoID),
		BroadcastAt:               timePtrFromSQLite(v.BroadcastAt),
		NextRetryAt:               timePtrFromSQLite(v.NextRetryAt),
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
		&row.DeletionKind,
		&row.DeleteRequestedAt,
		&row.RecordingType,
		&row.ForceH264,
		&row.Title,
		&row.CompletionKind,
		&row.Truncated,
		&row.TriggerScheduleID,
		&row.RetentionSourceScheduleID,
		&row.RetentionWindowHours,
		&row.Source,
		&row.TwitchVideoID,
		&row.BroadcastAt,
		&row.NextRetryAt,
	)
	return row, err
}

func sqliteCursorStartDownloadAt(cursor *repository.VideoPageCursor) sql.NullString {
	if cursor == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: sqlitetype.Format(cursor.StartDownloadAt), Valid: true}
}

func sqliteCursorID(cursor *repository.VideoPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.ID
}

// ListVideosForStorageScan bounds each query even if a caller passes an invalid limit.
func (a *SQLiteAdapter) ListVideosForStorageScan(ctx context.Context, afterID int64, limit int) ([]repository.StorageScanVideo, error) {
	if afterID < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid storage scan page")
	}
	rows, err := a.queries.ListVideosForStorageScan(ctx, sqlitegen.ListVideosForStorageScanParams{AfterID: afterID, PageSize: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list videos for storage scan: %w", err)
	}
	out := make([]repository.StorageScanVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.StorageScanVideo{VideoID: r.ID, Filename: r.Filename, Status: r.Status}
	}
	return out, nil
}

func (a *SQLiteAdapter) GetOpenVideoByTwitchVideoID(ctx context.Context, twitchVideoID string) (*repository.Video, error) {
	row, err := a.queries.GetOpenVideoByTwitchVideoID(ctx, sql.NullString{String: twitchVideoID, Valid: true})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoToDomain(row), nil
}

func (a *SQLiteAdapter) ListOpenVideosByTwitchVideoIDs(ctx context.Context, twitchVideoIDs []string) ([]repository.Video, error) {
	if len(twitchVideoIDs) == 0 {
		return []repository.Video{}, nil
	}
	ids := make([]sql.NullString, len(twitchVideoIDs))
	for i, id := range twitchVideoIDs {
		ids[i] = sql.NullString{String: id, Valid: true}
	}
	rows, err := a.queries.ListOpenVideosByTwitchVideoIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list open videos by twitch video ids: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) DeleteQueuedArchiveVideo(ctx context.Context, id int64) error {
	n, err := a.queries.DeleteQueuedArchiveVideo(ctx, id)
	if err != nil {
		return fmt.Errorf("sqlite delete queued archive video: %w", err)
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// GetVideoForStorageScan reuses the eligibility query, without treating zero as a wildcard.
func (a *SQLiteAdapter) GetVideoForStorageScan(ctx context.Context, id int64) (*repository.StorageScanVideo, error) {
	if id <= 0 {
		return nil, repository.ErrNotFound
	}
	rows, err := a.ListVideosForStorageScan(ctx, id-1, 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].VideoID != id {
		return nil, repository.ErrNotFound
	}
	return &rows[0], nil
}

func (a *SQLiteAdapter) TombstoneMissingVideo(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, repository.ErrNotFound
	}
	n, err := a.queries.TombstoneMissingVideo(ctx, id)
	return n > 0, err
}

func (a *SQLiteAdapter) RestoreMissingVideo(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrNotFound
	}
	n, err := a.queries.RestoreMissingVideo(ctx, id)
	if err != nil {
		return fmt.Errorf("sqlite restore missing video: %w", mapErr(err))
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (a *SQLiteAdapter) ListMissingTombstones(ctx context.Context, afterID int64, limit int) ([]repository.StorageScanVideo, error) {
	if afterID < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid storage scan page")
	}
	rows, err := a.queries.ListMissingTombstones(ctx, sqlitegen.ListMissingTombstonesParams{AfterID: afterID, PageSize: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list missing tombstones: %w", err)
	}
	out := make([]repository.StorageScanVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.StorageScanVideo{VideoID: r.ID, Filename: r.Filename, Status: r.Status}
	}
	return out, nil
}

func (a *SQLiteAdapter) ListVideosForStorageWitness(ctx context.Context, limit int) ([]repository.StorageScanVideo, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid storage witness sample")
	}
	rows, err := a.queries.ListVideosForStorageWitness(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("sqlite list storage witnesses: %w", err)
	}
	out := make([]repository.StorageScanVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.StorageScanVideo{VideoID: r.ID, Filename: r.Filename, Status: r.Status}
	}
	return out, nil
}

func (a *SQLiteAdapter) GetMissingTombstone(ctx context.Context, id int64) (*repository.StorageScanVideo, error) {
	if id <= 0 {
		return nil, repository.ErrNotFound
	}
	rows, err := a.ListMissingTombstones(ctx, id-1, 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].VideoID != id {
		return nil, repository.ErrNotFound
	}
	return &rows[0], nil
}

func (a *SQLiteAdapter) ListOpenVideosByStreamIDs(ctx context.Context, streamIDs []string) ([]repository.Video, error) {
	if len(streamIDs) == 0 {
		return []repository.Video{}, nil
	}
	ids := make([]sql.NullString, len(streamIDs))
	for i, id := range streamIDs {
		ids[i] = sql.NullString{String: id, Valid: true}
	}
	rows, err := a.queries.ListOpenVideosByStreamIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list open videos by stream ids: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) ListRecentArchiveFailures(ctx context.Context, since time.Time, limit int) ([]repository.Video, error) {
	rows, err := a.queries.ListRecentArchiveFailures(ctx, sqlitegen.ListRecentArchiveFailuresParams{DownloadedAt: sqliteTimePtr(&since), Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list recent archive failures: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) ListArchivesDueForRetry(ctx context.Context, now, after time.Time, afterID int64, limit int) ([]repository.Video, error) {
	rows, err := a.queries.ListArchivesDueForRetry(ctx, sqlitegen.ListArchivesDueForRetryParams{Now: sqliteTimePtr(&now), AfterTime: sqliteTimePtr(&after), AfterID: afterID, BatchLimit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list archives due for retry: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) MarkArchiveFailedForRetry(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool, nextRetryAt time.Time) error {
	return a.queries.MarkArchiveFailedForRetry(ctx, sqlitegen.MarkArchiveFailedForRetryParams{
		Error:          sql.NullString{String: errMsg, Valid: true},
		CompletionKind: completionKind,
		Truncated:      sqliteBool(truncated),
		NextRetryAt:    sqliteTimePtr(&nextRetryAt),
		ID:             id,
	})
}

func (a *SQLiteAdapter) RequeueArchiveVideo(ctx context.Context, id int64, jobID string, scheduledOnly bool) error {
	n, err := a.queries.RequeueArchiveVideo(ctx, sqlitegen.RequeueArchiveVideoParams{JobID: jobID, ID: id, ScheduledOnly: boolToInt64(scheduledOnly)})
	if err != nil {
		return fmt.Errorf("sqlite requeue archive video: %w", mapErr(err))
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (a *SQLiteAdapter) ClearArchiveRetry(ctx context.Context, id int64) error {
	n, err := a.queries.ClearArchiveRetry(ctx, id)
	if err != nil {
		return fmt.Errorf("sqlite clear archive retry: %w", err)
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (a *SQLiteAdapter) ListArchivesMissingPoster(ctx context.Context, since time.Time, afterID int64, limit int) ([]repository.Video, error) {
	if afterID < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid poster page")
	}
	rows, err := a.queries.ListArchivesMissingPoster(ctx, sqlitegen.ListArchivesMissingPosterParams{Since: sqliteTime(since), AfterID: afterID, PageSize: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list archives missing poster: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}

func (a *SQLiteAdapter) SetVideoThumbnailIfMissing(ctx context.Context, id int64, thumbnail string) (bool, error) {
	n, err := a.queries.SetVideoThumbnailIfMissing(ctx, sqlitegen.SetVideoThumbnailIfMissingParams{ID: id, Thumbnail: sql.NullString{String: thumbnail, Valid: true}})
	if err != nil {
		return false, fmt.Errorf("sqlite set video thumbnail if missing: %w", err)
	}
	return n > 0, nil
}
