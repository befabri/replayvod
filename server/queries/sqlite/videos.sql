-- name: GetVideo :one
SELECT * FROM videos WHERE id = ?;

-- name: GetVideoByJobID :one
SELECT * FROM videos WHERE job_id = ?;

-- name: ListVideosByJobIDs :many
SELECT * FROM videos WHERE job_id IN (sqlc.slice('job_ids'));

-- name: CreateVideo :one
INSERT INTO videos (
    job_id, filename, display_name, title, status, quality,
    broadcaster_id, stream_id, viewer_count, language, recording_type,
    force_h264, trigger_schedule_id, retention_source_schedule_id,
    retention_window_hours, source, twitch_video_id, broadcast_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateVideoStatus :exec
UPDATE videos SET status = ? WHERE id = ?;

-- name: UpdateVideoSelectedVariant :exec
UPDATE videos SET
    selected_quality = @quality,
    selected_fps = @fps
WHERE id = @id;

-- name: MarkVideoDone :exec
-- See postgres/videos.sql MarkVideoDone for the completion_kind /
-- truncated rationale.
UPDATE videos SET
    status = 'DONE',
    downloaded_at = datetime('now'),
    duration_seconds = ?,
    size_bytes = ?,
    -- A run that produced no frame (audio, monochrome video) keeps the poster
    -- the snapshotter or the archive fetch already stored.
    thumbnail = COALESCE(?, thumbnail),
    completion_kind = ?,
    truncated = ?
WHERE id = ?;

-- name: MarkVideoFailed :exec
-- See postgres/videos.sql MarkVideoFailed for the completion_kind /
-- truncated rationale.
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = datetime('now'),
    error = @err_msg,
    completion_kind = @completion_kind,
    truncated = @truncated
WHERE id = @id;

-- name: SetVideoThumbnail :exec
UPDATE videos SET thumbnail = ? WHERE id = ?;

-- name: ListVideos :many
-- Unified list query with optional status filter and enum-driven sort.
-- Bind params once in a CTE with explicit casts so sqlc's SQLite output stays
-- typed through the repeated CASE expressions.
WITH params AS (
    SELECT CAST(@status_filter AS text) AS status_filter,
           CAST(@sort_key AS text) AS sort_key,
           CAST(@row_limit AS integer) AS row_limit,
           CAST(@row_offset AS integer) AS row_offset
)
SELECT v.* FROM videos v
CROSS JOIN params
WHERE v.deleted_at IS NULL
  AND (params.status_filter = '' OR v.status = params.status_filter)
ORDER BY
  CASE WHEN params.sort_key = 'duration-desc'  THEN v.duration_seconds  END DESC NULLS LAST,
  CASE WHEN params.sort_key = 'duration-asc'   THEN v.duration_seconds  END ASC NULLS LAST,
  CASE WHEN params.sort_key = 'size-desc'      THEN v.size_bytes        END DESC NULLS LAST,
  CASE WHEN params.sort_key = 'size-asc'       THEN v.size_bytes        END ASC NULLS LAST,
  CASE WHEN params.sort_key = 'channel-asc'    THEN v.display_name      END ASC,
  CASE WHEN params.sort_key = 'channel-desc'   THEN v.display_name      END DESC,
  CASE WHEN params.sort_key = 'history_when-desc' THEN COALESCE(v.deleted_at, v.downloaded_at, v.start_download_at) END DESC,
  CASE WHEN params.sort_key = 'history_when-asc'  THEN COALESCE(v.deleted_at, v.downloaded_at, v.start_download_at) END ASC,
  CASE WHEN params.sort_key = 'created_at-asc' THEN v.start_download_at END ASC,
  v.start_download_at DESC,
  CASE WHEN params.sort_key LIKE '%-asc' THEN v.id END ASC,
  v.id DESC
LIMIT (SELECT row_limit FROM params) OFFSET (SELECT row_offset FROM params);

-- name: ListVideosByBroadcasterPage :many
WITH params AS (
    SELECT CAST(@broadcaster_id AS text) AS broadcaster_id,
           CAST(sqlc.narg('cursor_start_download_at') AS text) AS cursor_start_download_at,
           CAST(@cursor_id AS integer) AS cursor_id,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT v.* FROM videos v
CROSS JOIN params
WHERE v.broadcaster_id = params.broadcaster_id
  AND v.deleted_at IS NULL
  AND (
    params.cursor_start_download_at IS NULL
    OR v.start_download_at < params.cursor_start_download_at
    OR (v.start_download_at = params.cursor_start_download_at AND v.id < params.cursor_id)
  )
ORDER BY v.start_download_at DESC, v.id DESC
LIMIT (SELECT row_limit FROM params);

-- name: ListVideosByCategoryPage :many
WITH params AS (
    SELECT CAST(@category_id AS text) AS category_id,
           CAST(sqlc.narg('cursor_start_download_at') AS text) AS cursor_start_download_at,
           CAST(@cursor_id AS integer) AS cursor_id,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT v.* FROM videos v
CROSS JOIN params
INNER JOIN video_categories vc ON vc.video_id = v.id
WHERE vc.category_id = params.category_id
  AND v.deleted_at IS NULL
  AND (
    params.cursor_start_download_at IS NULL
    OR v.start_download_at < params.cursor_start_download_at
    OR (v.start_download_at = params.cursor_start_download_at AND v.id < params.cursor_id)
  )
ORDER BY v.start_download_at DESC, v.id DESC
LIMIT (SELECT row_limit FROM params);

-- name: SearchVideos :many
WITH q AS (
    SELECT
        unicode_lower(CAST(@query AS text)) AS term,
        unicode_lower(CAST(@query AS text)) || '%' AS prefix,
        '%' || unicode_lower(CAST(@query AS text)) || '%' AS contains,
        CAST(@limit AS integer) AS row_limit
),
title_matches AS (
    SELECT
        vt.video_id,
        MAX(unicode_lower(t.name) = q.term) AS title_exact,
        MAX(unicode_lower(t.name) LIKE q.prefix) AS title_prefix,
        MAX(unicode_lower(t.name) LIKE q.contains) AS title_contains
    FROM video_titles vt
    INNER JOIN titles t ON t.id = vt.title_id
    CROSS JOIN q
    GROUP BY vt.video_id
),
category_matches AS (
    SELECT
        vc.video_id,
        MAX(unicode_lower(c.name) = q.term) AS category_exact,
        MAX(unicode_lower(c.name) LIKE q.prefix) AS category_prefix,
        MAX(unicode_lower(c.name) LIKE q.contains) AS category_contains
    FROM video_categories vc
    INNER JOIN categories c ON c.id = vc.category_id
    CROSS JOIN q
    GROUP BY vc.video_id
),
matched AS (
    SELECT
        v.id,
        q.term = '' AS empty_query,
        unicode_lower(coalesce(v.title, '')) = q.term OR coalesce(tm.title_exact, 0) AS title_exact,
        unicode_lower(coalesce(v.title, '')) LIKE q.prefix OR coalesce(tm.title_prefix, 0) AS title_prefix,
        unicode_lower(coalesce(v.title, '')) LIKE q.contains OR coalesce(tm.title_contains, 0) AS title_contains,
        unicode_lower(coalesce(v.display_name, '')) = q.term
            OR unicode_lower(coalesce(ch.broadcaster_login, '')) = q.term
            OR unicode_lower(coalesce(ch.broadcaster_name, '')) = q.term AS channel_exact,
        unicode_lower(coalesce(v.display_name, '')) LIKE q.prefix
            OR unicode_lower(coalesce(ch.broadcaster_login, '')) LIKE q.prefix
            OR unicode_lower(coalesce(ch.broadcaster_name, '')) LIKE q.prefix AS channel_prefix,
        unicode_lower(coalesce(v.display_name, '')) LIKE q.contains
            OR unicode_lower(coalesce(ch.broadcaster_login, '')) LIKE q.contains
            OR unicode_lower(coalesce(ch.broadcaster_name, '')) LIKE q.contains AS channel_contains,
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
SELECT v.* FROM videos v
INNER JOIN matched m ON m.id = v.id
WHERE m.empty_query
   OR m.title_contains
   OR m.channel_contains
   OR m.category_contains
ORDER BY
    CASE
        WHEN m.empty_query THEN 7
        WHEN m.title_exact THEN 0
        WHEN m.title_prefix THEN 1
        WHEN m.channel_exact THEN 2
        WHEN m.channel_prefix THEN 3
        WHEN m.category_exact THEN 4
        WHEN m.category_prefix THEN 5
        ELSE 6
    END,
    v.start_download_at DESC,
    v.id DESC
LIMIT (SELECT row_limit FROM q);

-- name: ListVideosMissingThumbnail :many
SELECT * FROM videos WHERE status = 'DONE' AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: RequestVideoDelete :one
-- Queue an operator-requested deletion. Idempotent for already-queued live
-- terminal rows; active recordings must be cancelled first.
-- A missing-media tombstone may be removed permanently too.
UPDATE videos
SET delete_requested_at = COALESCE(delete_requested_at, datetime('now')),
    next_retry_at = NULL
WHERE id = ?
  AND (deleted_at IS NULL OR deletion_kind = 'missing')
  AND status IN ('DONE', 'FAILED')
RETURNING *;

-- name: SoftDeleteVideo :exec
-- See postgres/videos.sql SoftDeleteVideo.
UPDATE videos
SET deleted_at = datetime('now'),
    deletion_kind = CASE
      WHEN delete_requested_at IS NOT NULL THEN 'manual'
      ELSE @kind
    END,
    thumbnail = NULL,
    delete_requested_at = NULL
WHERE id = @id AND (deleted_at IS NULL OR deletion_kind = 'missing');

-- name: ListRetentionCandidates :many
-- Terminal, not-yet-tombstoned recordings whose creation-time retention policy
-- snapshot is due at @now. DONE rows own watchable artifacts; FAILED
-- partial/cancelled rows may own finalized parts, thumbnails, strips, and
-- snapshots. FAILED rows without salvage are excluded so retention does not
-- erase error-only diagnostics. Recordings without retention_window_hours are
-- explicitly outside retention, even if the same broadcaster currently has a
-- delete schedule. The strict due boundary mirrors retention.expiredVideoIDs;
-- keep both comparisons in lockstep so the SQL prefilter and Go invariant check
-- agree on "exactly at the deadline is still retained".
SELECT id, broadcaster_id, downloaded_at, retention_window_hours FROM videos
WHERE videos.id > sqlc.arg(after_id) AND deleted_at IS NULL
  AND delete_requested_at IS NULL
  AND downloaded_at IS NOT NULL
  AND retention_window_hours IS NOT NULL
  AND datetime(downloaded_at, '+' || retention_window_hours || ' hours') < @now
  AND (
    status = 'DONE'
    OR (status = 'FAILED' AND completion_kind IN ('partial', 'cancelled'))
  )
  AND NOT EXISTS (
    SELECT 1
    FROM recording_webhook_deliveries rwd
    WHERE rwd.video_id = videos.id
      AND rwd.test = 0
      AND rwd.status IN ('pending', 'delivering')
      AND rwd.frozen_parts = ''
  ) ORDER BY videos.id LIMIT sqlc.arg(limit);

-- name: ListVideosPendingManualDelete :many
-- Operator-requested deletions that are safe for the background worker to
-- finalize. The webhook frozen-parts guard mirrors retention: do not delete
-- video_parts until any pending/delivering delivery has captured them.
SELECT * FROM videos
WHERE id > CAST(sqlc.arg(after_id) AS BIGINT) AND (deleted_at IS NULL OR deletion_kind = 'missing')
  AND delete_requested_at IS NOT NULL
  AND status IN ('DONE', 'FAILED')
  AND NOT EXISTS (
    SELECT 1
    FROM recording_webhook_deliveries rwd
    WHERE rwd.video_id = videos.id
      AND rwd.test = 0
      AND rwd.status IN ('pending', 'delivering')
      AND rwd.frozen_parts = ''
  )
ORDER BY id ASC
LIMIT @limit;

-- name: CountVideosByStatus :one
SELECT COUNT(*) FROM videos WHERE status = ? AND deleted_at IS NULL;

-- name: StatisticsByStatus :many
SELECT status, COUNT(*) AS count FROM videos WHERE deleted_at IS NULL GROUP BY status;

-- name: StatisticsHistory :many
-- See queries/postgres/videos.sql for why this stays a plain group-by.
SELECT
    status,
    completion_kind,
    CAST((deleted_at IS NOT NULL) AS INTEGER) AS removed,
    CAST(COALESCE(deletion_kind, '') AS TEXT) AS deletion_kind,
    CAST(COUNT(*) AS INTEGER) AS count
FROM videos
WHERE status IN ('DONE', 'FAILED')
GROUP BY status, completion_kind, (deleted_at IS NOT NULL), COALESCE(deletion_kind, '');

-- name: StatisticsTotalsByBroadcaster :one
-- Per-channel rollup of finished recordings: count + summed bytes +
-- summed duration. Scopes the library totals to one broadcaster
-- so the watch page can render a "N recordings and X GB" line under the
-- channel name without paginating the full library client-side.
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(size_bytes), 0) AS INTEGER) AS total_size,
    CAST(COALESCE(SUM(duration_seconds), 0) AS REAL) AS total_duration
FROM videos
-- sqlc-sqlite v1.30 can truncate the final byte of this generated
-- const, so keep a tautology after the meaningful NULL predicate.
WHERE broadcaster_id = ? AND status = 'DONE' AND deleted_at IS NULL
  AND 1 = 1.00;

-- name: ListVideosForStorageScan :many
-- Bounded keyset page of terminal recordings safe to reconcile.
SELECT videos.id, videos.filename, videos.status FROM videos
WHERE deleted_at IS NULL
  AND delete_requested_at IS NULL
  AND next_retry_at IS NULL
  AND (
    status = 'DONE'
    OR (status = 'FAILED' AND EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id))
  )
  AND videos.id > CAST(@after_id AS INTEGER)
ORDER BY videos.id ASC LIMIT CAST(@limit AS INTEGER);

-- name: ListVideosForStorageWitness :many
-- Before initializing markerless storage, account for media even when a retry,
-- running capture, deletion request or reversible tombstone excludes scanning.
SELECT videos.id, videos.filename, videos.status FROM videos
WHERE (deleted_at IS NULL OR deletion_kind = 'missing')
  AND (status = 'DONE' OR EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id))
ORDER BY videos.id ASC LIMIT CAST(@page_size AS INTEGER);

-- name: TombstoneMissingVideo :execrows
-- Preserve objects and their metadata. A concurrent deletion request or state
-- transition wins; discovery must never turn into destructive deletion.
UPDATE videos SET deleted_at = datetime('now'), deletion_kind = 'missing'
WHERE deleted_at IS NULL
  AND delete_requested_at IS NULL
  AND next_retry_at IS NULL
  AND (
    status = 'DONE'
    OR (status = 'FAILED' AND EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id))
  )
  AND videos.id = ?;

-- name: GetOpenVideoByTwitchVideoID :one
-- An "open" archive is one that still counts against the one-row-per-VOD
-- rule: not removed, and either not failed or failed with a retry scheduled.
-- Mirrors idx_videos_open_twitch_video_id.
SELECT * FROM videos
WHERE twitch_video_id = ? AND deleted_at IS NULL
  AND (status <> 'FAILED' OR next_retry_at IS NOT NULL)
LIMIT 1;

-- name: ListOpenVideosByTwitchVideoIDs :many
SELECT * FROM videos
WHERE twitch_video_id IN (sqlc.slice('twitch_video_ids'))
  AND deleted_at IS NULL AND (status <> 'FAILED' OR next_retry_at IS NOT NULL);

-- name: ListOpenVideosByStreamIDs :many
-- Live recordings of the given broadcasts that still hold their media, so
-- the archive browser can tell a VOD was already captured live.
SELECT * FROM videos
WHERE stream_id IN (sqlc.slice('stream_ids'))
  AND source = 'live' AND deleted_at IS NULL AND status <> 'FAILED';

-- name: ListArchiveQueue :many
SELECT * FROM videos
WHERE source = 'vod' AND deleted_at IS NULL AND status IN ('PENDING', 'RUNNING')
ORDER BY start_download_at ASC, id ASC;

-- name: DeleteQueuedArchiveVideo :execrows
-- Only a queued archive can be dropped outright: nothing has been captured, so
-- there is no media and no tombstone to keep. Child rows cascade.
DELETE FROM videos WHERE id = ? AND source = 'vod' AND status = 'PENDING';

-- name: ListRecentArchiveFailures :many
SELECT * FROM videos
WHERE source = 'vod' AND deleted_at IS NULL AND status = 'FAILED' AND downloaded_at >= @since
ORDER BY downloaded_at DESC, id DESC LIMIT @limit;

-- name: ListArchivesDueForRetry :many
SELECT * FROM videos WHERE source='vod' AND deleted_at IS NULL AND status='FAILED'
 AND delete_requested_at IS NULL AND next_retry_at <= sqlc.arg(now)
 AND (next_retry_at,id) > (sqlc.arg(after),CAST(sqlc.arg(after_id) AS BIGINT))
ORDER BY next_retry_at,id LIMIT sqlc.arg(limit);

-- name: MarkArchiveFailedForRetry :exec
-- A transient archive failure: the row fails like any other, and the retry
-- time set in the same statement keeps it open under the one-row-per-VOD
-- rule so nobody can queue the same VOD twice while it waits.
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = datetime('now'),
    error = @err_msg,
    completion_kind = @completion_kind,
    truncated = @truncated,
    next_retry_at = @next_retry_at
WHERE id = @id AND source = 'vod';

-- name: RequeueArchiveVideo :execrows
-- Puts a failed archive back in the queue under a fresh job. scheduled_only
-- restricts the requeue to rows whose retry is still scheduled, so the pump
-- never revives a retry the operator cancelled a moment earlier.
UPDATE videos SET
    status = 'PENDING',
    job_id = @job_id,
    error = NULL,
    downloaded_at = NULL,
    completion_kind = 'complete',
    truncated = 0,
    next_retry_at = NULL
WHERE id = @id AND source = 'vod' AND status = 'FAILED' AND deleted_at IS NULL
  AND delete_requested_at IS NULL
  AND (CAST(@scheduled_only AS INTEGER) = 0 OR next_retry_at IS NOT NULL);

-- name: ClearArchiveRetry :execrows
UPDATE videos SET next_retry_at = NULL
WHERE id = ? AND source = 'vod' AND status = 'FAILED' AND next_retry_at IS NOT NULL;

-- name: ListArchivesMissingPoster :many
-- Keyset page of archives still without a poster, bounded to those queued
-- after the given instant so a VOD Twitch never renders is not looked up
-- forever. A failed archive that salvaged parts still shows in the library
-- and deserves its poster; one that never wrote media does not.
SELECT * FROM videos
WHERE source = 'vod' AND thumbnail IS NULL AND deleted_at IS NULL
  AND (status <> 'FAILED' OR EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id))
  AND twitch_video_id IS NOT NULL AND start_download_at >= @since
  AND videos.id > CAST(@after_id AS INTEGER)
ORDER BY id ASC LIMIT CAST(@limit AS INTEGER);

-- name: SetVideoThumbnailIfMissing :execrows
-- A poster never replaces a frame the pipeline already produced, and a row
-- removed while the poster was in flight stays without one.
UPDATE videos SET thumbnail = ? WHERE id = ? AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: RestoreMissingVideo :execrows
-- See postgres/videos.sql RestoreMissingVideo.
UPDATE videos SET deleted_at = NULL, deletion_kind = NULL
WHERE id = ?
  AND deleted_at IS NOT NULL
  AND deletion_kind = 'missing'
  AND delete_requested_at IS NULL;

-- name: ListMissingTombstones :many
-- See postgres/videos.sql ListMissingTombstones.
SELECT videos.id, videos.filename, videos.status FROM videos
WHERE deleted_at IS NOT NULL
  AND deletion_kind = 'missing'
  AND delete_requested_at IS NULL
  AND videos.id > CAST(@after_id AS INTEGER)
ORDER BY videos.id ASC LIMIT CAST(@limit AS INTEGER);
