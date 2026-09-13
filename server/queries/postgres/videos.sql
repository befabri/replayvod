-- name: GetVideo :one
SELECT * FROM videos WHERE id = $1;

-- name: GetVideoByJobID :one
SELECT * FROM videos WHERE job_id = $1;

-- name: ListVideosByJobIDs :many
SELECT * FROM videos WHERE job_id = ANY(@job_ids::text[]);

-- name: CreateVideo :one
INSERT INTO videos (
    job_id, filename, display_name, title, status, quality,
    broadcaster_id, stream_id, viewer_count, language, recording_type,
    force_h264, trigger_schedule_id, retention_source_schedule_id,
    retention_window_hours, source, twitch_video_id, broadcast_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
RETURNING *;

-- name: UpdateVideoStatus :exec
UPDATE videos SET status = $2 WHERE id = $1;

-- name: UpdateVideoSelectedVariant :exec
UPDATE videos SET
    selected_quality = $2,
    selected_fps = $3
WHERE id = $1;

-- name: MarkVideoDone :exec
-- completion_kind describes the artifact: 'complete' for a clean
-- run with no gaps, 'partial' when resume_state recorded a
-- restart_window_rolled (CDN dropped data we couldn't recover).
-- truncated is the orthogonal stop-boundary axis: true when the
-- recording stopped before the broadcast did (no EXT-X-ENDLIST seen,
-- or hadWindowRoll), false when the playlist's ENDLIST tag closed
-- the run naturally.
UPDATE videos SET
    status = 'DONE',
    downloaded_at = NOW(),
    duration_seconds = $2,
    size_bytes = $3,
    -- See sqlite/videos.sql MarkVideoDone: a frameless run keeps its poster.
    thumbnail = COALESCE($4, thumbnail),
    completion_kind = $5,
    truncated = $6
WHERE id = $1;

-- name: MarkVideoFailed :exec
-- completion_kind: 'cancelled' for operator-initiated stops, 'partial'
-- when at least one part was finalized before the failure (some
-- watchable output exists), 'complete' otherwise (no salvage). UI
-- renders a grey CANCELLED badge for cancelled, yellow PARTIAL for
-- partial, red FAILED for complete.
-- truncated is true for any FAILED run — the broadcast was still
-- live when we stopped recording (otherwise the run would have
-- transitioned to DONE via the natural ENDLIST path).
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = NOW(),
    error = $2,
    completion_kind = $3,
    truncated = $4
WHERE id = $1;

-- name: SetVideoThumbnail :exec
UPDATE videos SET thumbnail = $2 WHERE id = $1;

-- name: ListVideos :many
-- Unified list query with optional status filter and enum-driven sort.
-- @status_filter = '' disables the status filter; otherwise filters exactly.
-- @sort_key combines column + direction ("duration-desc", "size-asc", etc.).
-- Unmatched CASE branches evaluate to NULL uniformly across rows, so PG
-- treats them as tied. The terminal `start_download_at DESC` is both the
-- explicit 'created_at-desc' sort (matched by the CASE above it) and the
-- fallthrough for empty/unrecognized sort_key values.
SELECT * FROM videos
WHERE deleted_at IS NULL
  AND (@status_filter::text = '' OR status = @status_filter::text)
ORDER BY
  CASE WHEN @sort_key::text = 'duration-desc'  THEN duration_seconds  END DESC NULLS LAST,
  CASE WHEN @sort_key::text = 'duration-asc'   THEN duration_seconds  END ASC NULLS LAST,
  CASE WHEN @sort_key::text = 'size-desc'      THEN size_bytes        END DESC NULLS LAST,
  CASE WHEN @sort_key::text = 'size-asc'       THEN size_bytes        END ASC NULLS LAST,
  CASE WHEN @sort_key::text = 'channel-asc'    THEN display_name      END ASC,
  CASE WHEN @sort_key::text = 'channel-desc'   THEN display_name      END DESC,
  CASE WHEN @sort_key::text = 'history_when-desc' THEN COALESCE(deleted_at, downloaded_at, start_download_at) END DESC,
  CASE WHEN @sort_key::text = 'history_when-asc'  THEN COALESCE(deleted_at, downloaded_at, start_download_at) END ASC,
  CASE WHEN @sort_key::text = 'created_at-asc' THEN start_download_at END ASC,
  start_download_at DESC,
  -- Tiebreaker direction tracks the primary sort intent: when the
  -- caller asked for ASC (oldest-first), break ties with id ASC so
  -- same-second-timestamp rows stay oldest-first. Otherwise id DESC
  -- ("newer wins") lines up with the default created-desc behavior.
  -- Matters on SQLite where datetime('now') is second-precision;
  -- on PG microsecond timestamps make ties near-unreachable but we
  -- keep the two dialects observably identical.
  CASE WHEN @sort_key::text LIKE '%-asc' THEN id END ASC,
  id DESC
LIMIT @row_limit OFFSET @row_offset;

-- name: ListVideosByBroadcasterPage :many
SELECT v.* FROM videos v
WHERE v.broadcaster_id = @broadcaster_id::text
  AND v.deleted_at IS NULL
  AND (
    sqlc.narg('cursor_start_download_at')::timestamptz IS NULL
    OR v.start_download_at < sqlc.narg('cursor_start_download_at')::timestamptz
    OR (v.start_download_at = sqlc.narg('cursor_start_download_at')::timestamptz AND v.id < @cursor_id::bigint)
  )
ORDER BY v.start_download_at DESC, v.id DESC
LIMIT @row_limit;

-- name: ListVideosByCategoryPage :many
SELECT v.* FROM videos v
INNER JOIN video_categories vc ON vc.video_id = v.id
WHERE vc.category_id = @category_id::text
  AND v.deleted_at IS NULL
  AND (
    sqlc.narg('cursor_start_download_at')::timestamptz IS NULL
    OR v.start_download_at < sqlc.narg('cursor_start_download_at')::timestamptz
    OR (v.start_download_at = sqlc.narg('cursor_start_download_at')::timestamptz AND v.id < @cursor_id::bigint)
  )
ORDER BY v.start_download_at DESC, v.id DESC
LIMIT @row_limit;

-- name: SearchVideos :many
WITH q AS (
    SELECT
        lower(@query::text) AS term,
        lower(@query::text) || '%' AS prefix,
        '%' || lower(@query::text) || '%' AS contains
),
matched AS (
    SELECT
        v.id,
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
LIMIT @row_limit;

-- name: ListVideosMissingThumbnail :many
SELECT * FROM videos WHERE status = 'DONE' AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: RequestVideoDelete :one
-- Queue an operator-requested deletion. Idempotent for already-queued live
-- terminal rows; active recordings must be cancelled first.
-- A missing-media tombstone may be removed permanently too.
UPDATE videos
SET delete_requested_at = COALESCE(delete_requested_at, NOW()),
    next_retry_at = NULL
WHERE id = $1
  AND (deleted_at IS NULL OR deletion_kind = 'missing')
  AND status IN ('DONE', 'FAILED')
RETURNING *;

-- name: SoftDeleteVideo :exec
-- Tombstone a recording after its objects were deleted.
UPDATE videos
SET deleted_at = NOW(),
    deletion_kind = CASE
      WHEN delete_requested_at IS NOT NULL THEN 'manual'
      ELSE $2
    END,
    thumbnail = NULL,
    delete_requested_at = NULL
WHERE id = $1 AND (deleted_at IS NULL OR deletion_kind = 'missing');

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
  AND downloaded_at + (retention_window_hours * INTERVAL '1 hour') < @now::timestamptz
  AND (
    status = 'DONE'
    OR (status = 'FAILED' AND completion_kind IN ('partial', 'cancelled'))
  )
  AND NOT EXISTS (
    SELECT 1
    FROM recording_webhook_deliveries rwd
    WHERE rwd.video_id = videos.id
      AND rwd.test = FALSE
      AND rwd.status IN ('pending', 'delivering')
      AND rwd.frozen_parts = ''
  ) ORDER BY videos.id LIMIT sqlc.arg(batch_limit);

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
      AND rwd.test = FALSE
      AND rwd.status IN ('pending', 'delivering')
      AND rwd.frozen_parts = ''
  )
ORDER BY id ASC
LIMIT @row_limit;

-- name: CountVideosByStatus :one
SELECT COUNT(*) FROM videos WHERE status = $1 AND deleted_at IS NULL;

-- name: StatisticsByStatus :many
SELECT status, COUNT(*) AS count FROM videos WHERE deleted_at IS NULL GROUP BY status;

-- name: StatisticsHistory :many
-- Terminal recordings bucketed for the download-history tabs: by status, by
-- completion kind, and by whether the media is still on disk. Cancelled runs
-- are FAILED rows carrying completion_kind 'cancelled', so the SQL stays a
-- plain group-by and the outcome vocabulary is folded in Go, where the rule
-- lives once.
SELECT
    status,
    completion_kind,
    (deleted_at IS NOT NULL)::BOOLEAN AS removed,
    COALESCE(deletion_kind, '')::TEXT AS deletion_kind,
    COUNT(*) AS count
FROM videos
WHERE status IN ('DONE', 'FAILED')
GROUP BY status, completion_kind, (deleted_at IS NOT NULL), COALESCE(deletion_kind, '');

-- name: StatisticsTotalsByBroadcaster :one
-- Per-channel rollup of finished recordings: count + summed bytes +
-- summed duration. Scopes the library totals to one broadcaster
-- so the watch page can render a "N recordings · X GB" line under the
-- channel name without paginating the full library client-side.
SELECT
    COUNT(*)::BIGINT AS total,
    COALESCE(SUM(size_bytes), 0)::BIGINT AS total_size,
    COALESCE(SUM(duration_seconds), 0)::DOUBLE PRECISION AS total_duration
FROM videos
WHERE broadcaster_id = $1 AND status = 'DONE' AND deleted_at IS NULL;

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
  AND videos.id > @after_id::bigint
ORDER BY videos.id ASC LIMIT @page_size::int;

-- name: ListVideosForStorageWitness :many
-- Before initializing markerless storage, account for media even when a retry,
-- running capture, deletion request or reversible tombstone excludes scanning.
SELECT videos.id, videos.filename, videos.status FROM videos
WHERE (deleted_at IS NULL OR deletion_kind = 'missing')
  AND (status = 'DONE' OR EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id))
ORDER BY videos.id ASC LIMIT @page_size::int;

-- name: TombstoneMissingVideo :execrows
-- Preserve objects and their metadata. A concurrent deletion request or state
-- transition wins; discovery must never turn into destructive deletion.
UPDATE videos SET deleted_at = NOW(), deletion_kind = 'missing'
WHERE deleted_at IS NULL
  AND delete_requested_at IS NULL
  AND next_retry_at IS NULL
  AND (
    status = 'DONE'
    OR (status = 'FAILED' AND EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id))
  )
  AND videos.id = $1;

-- name: GetOpenVideoByTwitchVideoID :one
-- See sqlite/videos.sql GetOpenVideoByTwitchVideoID.
SELECT * FROM videos
WHERE twitch_video_id = $1 AND deleted_at IS NULL
  AND (status <> 'FAILED' OR next_retry_at IS NOT NULL)
LIMIT 1;

-- name: ListOpenVideosByTwitchVideoIDs :many
SELECT * FROM videos
WHERE twitch_video_id = ANY(@twitch_video_ids::text[])
  AND deleted_at IS NULL AND (status <> 'FAILED' OR next_retry_at IS NOT NULL);

-- name: ListOpenVideosByStreamIDs :many
-- See sqlite/videos.sql ListOpenVideosByStreamIDs.
SELECT * FROM videos
WHERE stream_id = ANY(@stream_ids::text[])
  AND source = 'live' AND deleted_at IS NULL AND status <> 'FAILED';

-- name: ListArchiveQueue :many
SELECT * FROM videos
WHERE source = 'vod' AND deleted_at IS NULL AND status IN ('PENDING', 'RUNNING')
ORDER BY start_download_at ASC, id ASC;

-- name: DeleteQueuedArchiveVideo :execrows
-- See sqlite/videos.sql DeleteQueuedArchiveVideo.
DELETE FROM videos WHERE id = $1 AND source = 'vod' AND status = 'PENDING';

-- name: ListRecentArchiveFailures :many
SELECT * FROM videos
WHERE source = 'vod' AND deleted_at IS NULL AND status = 'FAILED' AND downloaded_at >= $1
ORDER BY downloaded_at DESC, id DESC LIMIT $2;

-- name: ListArchivesDueForRetry :many
SELECT * FROM videos WHERE source='vod' AND deleted_at IS NULL AND status='FAILED'
 AND delete_requested_at IS NULL AND next_retry_at <= sqlc.arg(now)::timestamptz
 AND (next_retry_at,id) > (sqlc.arg(after_time)::timestamptz,CAST(sqlc.arg(after_id) AS BIGINT))
ORDER BY next_retry_at,id LIMIT sqlc.arg(batch_limit);

-- name: MarkArchiveFailedForRetry :exec
-- See sqlite/videos.sql MarkArchiveFailedForRetry.
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = NOW(),
    error = $2,
    completion_kind = $3,
    truncated = $4,
    next_retry_at = $5
WHERE id = $1 AND source = 'vod';

-- name: RequeueArchiveVideo :execrows
-- See sqlite/videos.sql RequeueArchiveVideo.
UPDATE videos SET
    status = 'PENDING',
    job_id = @job_id,
    error = NULL,
    downloaded_at = NULL,
    completion_kind = 'complete',
    truncated = FALSE,
    next_retry_at = NULL
WHERE id = @id AND source = 'vod' AND status = 'FAILED' AND deleted_at IS NULL
  AND delete_requested_at IS NULL
  AND (NOT @scheduled_only::boolean OR next_retry_at IS NOT NULL);

-- name: ClearArchiveRetry :execrows
UPDATE videos SET next_retry_at = NULL
WHERE id = $1 AND source = 'vod' AND status = 'FAILED' AND next_retry_at IS NOT NULL;

-- name: ListArchivesMissingPoster :many
-- See sqlite/videos.sql ListArchivesMissingPoster.
SELECT * FROM videos
WHERE source = 'vod' AND thumbnail IS NULL AND deleted_at IS NULL
  AND (status <> 'FAILED' OR EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id))
  AND twitch_video_id IS NOT NULL AND start_download_at >= @since::timestamptz
  AND videos.id > @after_id::bigint
ORDER BY id ASC LIMIT @page_size::int;

-- name: SetVideoThumbnailIfMissing :execrows
-- A poster never replaces a frame the pipeline already produced, and a row
-- removed while the poster was in flight stays without one.
UPDATE videos SET thumbnail = $2 WHERE id = $1 AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: RestoreMissingVideo :execrows
-- Bring a missing-media tombstone back into the library once its media is
-- present again. Only the missing kind is reversible; a queued manual delete
-- wins.
UPDATE videos SET deleted_at = NULL, deletion_kind = NULL
WHERE id = $1
  AND deleted_at IS NOT NULL
  AND deletion_kind = 'missing'
  AND delete_requested_at IS NULL;

-- name: ListMissingTombstones :many
-- Bounded keyset page of reversible tombstones for the scan's restore phase.
SELECT videos.id, videos.filename, videos.status FROM videos
WHERE deleted_at IS NOT NULL
  AND deletion_kind = 'missing'
  AND delete_requested_at IS NULL
  AND videos.id > @after_id::bigint
ORDER BY videos.id ASC LIMIT @page_size::int;
