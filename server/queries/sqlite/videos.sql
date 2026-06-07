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
    retention_window_hours
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateVideoStatus :exec
UPDATE videos SET status = ? WHERE id = ?;

-- name: UpdateVideoSelectedVariant :exec
UPDATE videos SET
    selected_quality = ?,
    selected_fps = ?
WHERE id = ?;

-- name: MarkVideoDone :exec
-- See postgres/videos.sql MarkVideoDone for the completion_kind /
-- truncated rationale.
UPDATE videos SET
    status = 'DONE',
    downloaded_at = datetime('now'),
    duration_seconds = ?,
    size_bytes = ?,
    thumbnail = ?,
    completion_kind = ?,
    truncated = ?
WHERE id = ?;

-- name: MarkVideoFailed :exec
-- See postgres/videos.sql MarkVideoFailed for the completion_kind /
-- truncated rationale.
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = datetime('now'),
    error = ?,
    completion_kind = ?,
    truncated = ?
WHERE id = ?;

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
        lower(CAST(@query AS text)) AS term,
        lower(CAST(@query AS text)) || '%' AS prefix,
        '%' || lower(CAST(@query AS text)) || '%' AS contains,
        CAST(@row_limit AS integer) AS row_limit
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
        v.id,
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
UPDATE videos
SET delete_requested_at = COALESCE(delete_requested_at, datetime('now'))
WHERE id = ?
  AND deleted_at IS NULL
  AND status IN ('DONE', 'FAILED')
RETURNING *;

-- name: SoftDeleteVideo :exec
-- Tombstone a recording. deletion_kind records why ('retention' | 'manual').
UPDATE videos
SET deleted_at = datetime('now'),
    deletion_kind = CASE
      WHEN delete_requested_at IS NOT NULL THEN 'manual'
      ELSE ?2
    END,
    delete_requested_at = NULL
WHERE id = ?1 AND deleted_at IS NULL;

-- name: ListFinishedVideosForRetention :many
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
WHERE deleted_at IS NULL
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
  );

-- name: ListVideosPendingManualDelete :many
-- Operator-requested deletions that are safe for the background worker to
-- finalize. The webhook frozen-parts guard mirrors retention: do not delete
-- video_parts until any pending/delivering delivery has captured them.
SELECT * FROM videos
WHERE deleted_at IS NULL
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
ORDER BY delete_requested_at ASC, id ASC
LIMIT @row_limit;

-- name: CountVideosByStatus :one
SELECT COUNT(*) FROM videos WHERE status = ? AND deleted_at IS NULL;

-- name: StatisticsByStatus :many
SELECT status, COUNT(*) AS count FROM videos WHERE deleted_at IS NULL GROUP BY status;

-- StatisticsTotals is split across four atomic queries instead of
-- one combined SELECT. The combined form (with CASE WHEN aggregates
-- in a multi-column SELECT list) triggers a sqlc-on-SQLite codegen
-- bug that truncates trailing chars off subsequent query consts
-- (StatisticsTotalsByBroadcaster ends up with `IS NUL` instead of
-- `IS NULL`). Splitting keeps each query small enough that the
-- parser doesn't trip; the adapter combines them into a single
-- VideoStatsTotals struct. Postgres still uses the single-query
-- form; see queries/postgres/videos.sql.

-- name: StatisticsTotalsDoneOnly :one
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(size_bytes), 0) AS INTEGER) AS total_size,
    CAST(COALESCE(SUM(duration_seconds), 0) AS REAL) AS total_duration
FROM videos WHERE status = 'DONE' AND deleted_at IS NULL;

-- name: StatisticsThisWeek :one
SELECT CAST(COUNT(*) AS INTEGER) AS this_week
FROM videos
WHERE deleted_at IS NULL AND start_download_at >= datetime('now', '-7 days');

-- name: StatisticsIncomplete :one
SELECT CAST(COUNT(*) AS INTEGER) AS incomplete
FROM videos
WHERE deleted_at IS NULL AND (completion_kind = 'partial' OR truncated);

-- name: StatisticsChannels :one
SELECT CAST(COUNT(DISTINCT broadcaster_id) AS INTEGER) AS channels
FROM videos
WHERE deleted_at IS NULL;

-- name: StatisticsRemoved :one
-- Count of tombstoned (removed) recordings; powers the History "Removed" tab.
SELECT CAST(COUNT(*) AS INTEGER) AS removed
FROM videos
WHERE deleted_at IS NOT NULL;

-- name: StatisticsWatchLater :one
SELECT CAST(COUNT(*) AS INTEGER) AS watch_later
FROM videos v
INNER JOIN video_user_states vus ON vus.video_id = v.id
WHERE v.deleted_at IS NULL
  AND CAST(@user_id AS text) <> ''
  AND vus.user_id = CAST(@user_id AS text)
  AND vus.watch_later = 1;

-- name: StatisticsUnwatched :one
SELECT CAST(COUNT(*) AS INTEGER) AS unwatched
FROM videos v
LEFT JOIN video_user_states vus
  ON vus.video_id = v.id AND vus.user_id = CAST(@user_id AS text)
WHERE v.deleted_at IS NULL
  AND v.status = 'DONE'
  AND CAST(@user_id AS text) <> ''
  AND vus.watched_at IS NULL;

-- name: StatisticsTotalsByBroadcaster :one
-- Per-channel rollup of finished recordings: count + summed bytes +
-- summed duration. Mirrors StatisticsTotals scoped to one broadcaster
-- so the watch page can render a "N recordings · X GB" line under the
-- channel name without paginating the full library client-side.
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(size_bytes), 0) AS INTEGER) AS total_size,
    CAST(COALESCE(SUM(duration_seconds), 0) AS REAL) AS total_duration
FROM videos
WHERE broadcaster_id = ? AND status = 'DONE' AND deleted_at IS NULL;
