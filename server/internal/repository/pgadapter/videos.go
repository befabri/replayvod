package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/jackc/pgx/v5"
)

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

// closeOpenVideoMetadataSpansWith accepts the terminal update's transaction so
// metadata spans and video status commit together.
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
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: v.RecordingType,
		Quality:       v.Quality,
		ForceH264:     v.ForceH264,
	})
	row, err := a.queries.CreateVideo(ctx, pggen.CreateVideoParams{
		JobID:                     v.JobID,
		Filename:                  v.Filename,
		DisplayName:               v.DisplayName,
		Title:                     v.Title,
		Status:                    v.Status,
		Quality:                   settings.Quality,
		BroadcasterID:             v.BroadcasterID,
		StreamID:                  v.StreamID,
		ViewerCount:               int32(v.ViewerCount),
		Language:                  v.Language,
		RecordingType:             settings.RecordingType,
		ForceH264:                 settings.ForceH264,
		TriggerScheduleID:         v.TriggerScheduleID,
		RetentionSourceScheduleID: v.RetentionSourceScheduleID,
		RetentionWindowHours:      int64PtrToInt32Ptr(v.RetentionWindowHours),
		Source:                    repository.VideoSourceOrLive(v.Source),
		TwitchVideoID:             v.TwitchVideoID,
		BroadcastAt:               v.BroadcastAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create video: %w", mapErr(err))
	}
	return pgVideoToDomain(row), nil
}

func (a *PGAdapter) UpdateVideoSelectedVariant(ctx context.Context, id int64, quality string, fps *float64) error {
	var qualityPtr *string
	if quality != "" {
		qualityPtr = &quality
	}
	return a.queries.UpdateVideoSelectedVariant(ctx, pggen.UpdateVideoSelectedVariantParams{
		ID:      id,
		Quality: qualityPtr,
		Fps:     fps,
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
			ErrMsg:         &errMsg,
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
			ErrMsg:         &errMsg,
			CompletionKind: completionKind,
			Truncated:      truncated,
		}); err != nil {
			return err
		}
		return pgCreateRecordingWebhookDeliveryIfEnabled(ctx, q, delivery)
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
	rows, err := a.queries.SearchVideos(ctx, pggen.SearchVideosParams{
		Query: query,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg search videos: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.queries.ListVideosByBroadcasterPage(ctx, pggen.ListVideosByBroadcasterPageParams{
		BroadcasterID:         broadcasterID,
		CursorStartDownloadAt: pgCursorStartDownloadAt(cursor),
		CursorID:              pgCursorID(cursor),
		RowLimit:              int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list videos by broadcaster: %w", err)
	}
	items := pgVideosToDomain(rows)
	return repository.ToVideoPage(items, limit), nil
}

func (a *PGAdapter) ListVideosByCategory(ctx context.Context, categoryID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	rows, err := a.queries.ListVideosByCategoryPage(ctx, pggen.ListVideosByCategoryPageParams{
		CategoryID:            categoryID,
		CursorStartDownloadAt: pgCursorStartDownloadAt(cursor),
		CursorID:              pgCursorID(cursor),
		RowLimit:              int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list videos by category: %w", err)
	}
	items := pgVideosToDomain(rows)
	return repository.ToVideoPage(items, limit), nil
}

func (a *PGAdapter) ListVideosPendingManualDelete(ctx context.Context, afterID int64, limit int) ([]repository.Video, error) {
	if limit <= 0 {
		return []repository.Video{}, nil
	}
	rows, err := a.queries.ListVideosPendingManualDelete(ctx, pggen.ListVideosPendingManualDeleteParams{AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list videos pending manual delete: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) SoftDeleteVideo(ctx context.Context, id int64, kind string) error {
	return a.queries.SoftDeleteVideo(ctx, pggen.SoftDeleteVideoParams{ID: id, Kind: &kind})
}

func (a *PGAdapter) ListRetentionCandidates(ctx context.Context, now time.Time, afterID int64, limit int) ([]repository.RetentionVideo, error) {
	rows, err := a.queries.ListRetentionCandidates(ctx, pggen.ListRetentionCandidatesParams{Now: now, AfterID: afterID, Limit: int32(limit)})
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

func (a *PGAdapter) FinalizeDelete(ctx context.Context, videoID int64, kind string) error {
	return a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		if err := q.SoftDeleteVideo(ctx, pggen.SoftDeleteVideoParams{ID: videoID, Kind: &kind}); err != nil {
			return fmt.Errorf("pg tombstone video: %w", err)
		}
		if err := q.DeleteVideoParts(ctx, videoID); err != nil {
			return fmt.Errorf("pg delete parts: %w", err)
		}
		return nil
	})
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

func (a *PGAdapter) VideoStatsHistory(ctx context.Context) ([]repository.VideoStatsHistoryBucket, error) {
	rows, err := a.queries.StatisticsHistory(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg video stats history: %w", err)
	}
	out := make([]repository.VideoStatsHistoryBucket, len(rows))
	for i, r := range rows {
		out[i] = repository.VideoStatsHistoryBucket{
			Status:         r.Status,
			CompletionKind: r.CompletionKind,
			Removed:        r.Removed,
			DeletionKind:   r.DeletionKind,
			Count:          r.Count,
		}
	}
	return out, nil
}

func (a *PGAdapter) VideoStatsTotals(ctx context.Context, userID string) (*repository.VideoStatsTotals, error) {
	query, args := repository.BuildVideoStatsTotalsQuery(userID, repository.VideoPageDialect{Postgres: true})
	var totals repository.VideoStatsTotals
	if err := a.db.QueryRow(ctx, query, args...).Scan(
		&totals.Total, &totals.TotalSize, &totals.TotalDuration,
		&totals.ThisWeek, &totals.Incomplete, &totals.Channels, &totals.Removed,
		&totals.WatchLater, &totals.Unwatched, &totals.ContinueWatching,
	); err != nil {
		return nil, fmt.Errorf("pg video stats totals: %w", err)
	}
	return &totals, nil
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
		DeleteRequestedAt:         v.DeleteRequestedAt,
		DeletionKind:              v.DeletionKind,
		RecordingType:             v.RecordingType,
		ForceH264:                 v.ForceH264,
		TriggerScheduleID:         v.TriggerScheduleID,
		RetentionSourceScheduleID: v.RetentionSourceScheduleID,
		RetentionWindowHours:      int32PtrToInt64Ptr(v.RetentionWindowHours),
		CompletionKind:            v.CompletionKind,
		Truncated:                 v.Truncated,
		Source:                    v.Source,
		TwitchVideoID:             v.TwitchVideoID,
		BroadcastAt:               v.BroadcastAt,
		NextRetryAt:               v.NextRetryAt,
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
		var lastProgressAtMs *int64
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
			&lastProgressAtMs,
		); err != nil {
			return nil, err
		}
		video := pgVideoToDomain(row)
		video.LastProgressAtMs = lastProgressAtMs
		items = append(items, *video)
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

// ListVideosForStorageScan bounds each query even if a caller passes an invalid limit.
func (a *PGAdapter) ListVideosForStorageScan(ctx context.Context, afterID int64, limit int) ([]repository.StorageScanVideo, error) {
	if afterID < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid storage scan page")
	}
	rows, err := a.queries.ListVideosForStorageScan(ctx, pggen.ListVideosForStorageScanParams{AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list videos for storage scan: %w", err)
	}
	out := make([]repository.StorageScanVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.StorageScanVideo{VideoID: r.ID, Filename: r.Filename, Status: r.Status}
	}
	return out, nil
}

func (a *PGAdapter) ListOpenVideosByTwitchVideoIDs(ctx context.Context, twitchVideoIDs []string) ([]repository.Video, error) {
	if len(twitchVideoIDs) == 0 {
		return []repository.Video{}, nil
	}
	rows, err := a.queries.ListOpenVideosByTwitchVideoIDs(ctx, twitchVideoIDs)
	if err != nil {
		return nil, fmt.Errorf("pg list open videos by twitch video ids: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

// GetVideoForStorageScan reuses the eligibility query, without treating zero as a wildcard.
func (a *PGAdapter) GetVideoForStorageScan(ctx context.Context, id int64) (*repository.StorageScanVideo, error) {
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

func (a *PGAdapter) TombstoneMissingVideo(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, repository.ErrNotFound
	}
	n, err := a.queries.TombstoneMissingVideo(ctx, id)
	return n > 0, err
}

func (a *PGAdapter) RestoreMissingVideo(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrNotFound
	}
	n, err := a.queries.RestoreMissingVideo(ctx, id)
	if err != nil {
		return fmt.Errorf("pg restore missing video: %w", mapErr(err))
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (a *PGAdapter) ListMissingTombstones(ctx context.Context, afterID int64, limit int) ([]repository.StorageScanVideo, error) {
	if afterID < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid storage scan page")
	}
	rows, err := a.queries.ListMissingTombstones(ctx, pggen.ListMissingTombstonesParams{AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list missing tombstones: %w", err)
	}
	out := make([]repository.StorageScanVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.StorageScanVideo{VideoID: r.ID, Filename: r.Filename, Status: r.Status}
	}
	return out, nil
}

func (a *PGAdapter) ListVideosForStorageWitness(ctx context.Context, limit int) ([]repository.StorageScanVideo, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid storage witness sample")
	}
	rows, err := a.queries.ListVideosForStorageWitness(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("pg list storage witnesses: %w", err)
	}
	out := make([]repository.StorageScanVideo, len(rows))
	for i, r := range rows {
		out[i] = repository.StorageScanVideo{VideoID: r.ID, Filename: r.Filename, Status: r.Status}
	}
	return out, nil
}

func (a *PGAdapter) GetMissingTombstone(ctx context.Context, id int64) (*repository.StorageScanVideo, error) {
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

func (a *PGAdapter) ListOpenVideosByStreamIDs(ctx context.Context, streamIDs []string) ([]repository.Video, error) {
	if len(streamIDs) == 0 {
		return []repository.Video{}, nil
	}
	rows, err := a.queries.ListOpenVideosByStreamIDs(ctx, streamIDs)
	if err != nil {
		return nil, fmt.Errorf("pg list open videos by stream ids: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListRecentArchiveFailures(ctx context.Context, since time.Time, limit int) ([]repository.Video, error) {
	rows, err := a.queries.ListRecentArchiveFailures(ctx, pggen.ListRecentArchiveFailuresParams{Since: &since, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list recent archive failures: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) ListArchivesDueForRetry(ctx context.Context, now, after time.Time, afterID int64, limit int) ([]repository.Video, error) {
	rows, err := a.queries.ListArchivesDueForRetry(ctx, pggen.ListArchivesDueForRetryParams{Now: now, After: after, AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list archives due for retry: %w", err)
	}
	return pgVideosToDomain(rows), nil
}

func (a *PGAdapter) MarkArchiveFailedForRetry(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool, nextRetryAt time.Time) error {
	return a.queries.MarkArchiveFailedForRetry(ctx, pggen.MarkArchiveFailedForRetryParams{
		ID:             id,
		ErrMsg:         &errMsg,
		CompletionKind: completionKind,
		Truncated:      truncated,
		NextRetryAt:    &nextRetryAt,
	})
}

func (a *PGAdapter) ListArchivesMissingPoster(ctx context.Context, since time.Time, afterID int64, limit int) ([]repository.Video, error) {
	if afterID < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid poster page")
	}
	rows, err := a.queries.ListArchivesMissingPoster(ctx, pggen.ListArchivesMissingPosterParams{Since: since, AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list archives missing poster: %w", err)
	}
	return pgVideosToDomain(rows), nil
}
