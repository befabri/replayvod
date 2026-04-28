-- name: GetVideo :one
SELECT * FROM videos WHERE id = $1;

-- name: GetVideoByJobID :one
SELECT * FROM videos WHERE job_id = $1;

-- name: CreateVideo :one
INSERT INTO videos (
    job_id, filename, display_name, title, status, quality,
    broadcaster_id, stream_id, viewer_count, language, recording_type,
    force_h264
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
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
    thumbnail = $4,
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

-- name: ListVideosMissingThumbnail :many
SELECT * FROM videos WHERE status = 'DONE' AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: SoftDeleteVideo :exec
UPDATE videos SET deleted_at = NOW() WHERE id = $1;

-- name: CountVideosByStatus :one
SELECT COUNT(*) FROM videos WHERE status = $1 AND deleted_at IS NULL;

-- name: StatisticsByStatus :many
SELECT status, COUNT(*) AS count FROM videos WHERE deleted_at IS NULL GROUP BY status;

-- name: StatisticsTotals :one
-- Library-wide rollups. Total / size / duration restrict to DONE rows
-- (these are the user-visible numbers in the page subtitle); the two
-- FILTER-counted columns drive the videos page tab counters and run
-- across all non-deleted rows.
SELECT
    COUNT(*) FILTER (WHERE status = 'DONE')::BIGINT AS total,
    COALESCE(SUM(size_bytes) FILTER (WHERE status = 'DONE'), 0)::BIGINT AS total_size,
    COALESCE(SUM(duration_seconds) FILTER (WHERE status = 'DONE'), 0)::DOUBLE PRECISION AS total_duration,
    COUNT(*) FILTER (WHERE start_download_at >= now() - interval '7 days')::BIGINT AS this_week,
    -- Mirrors the videos page Partial tab predicate exactly: any
    -- recording that didn't capture the full broadcast, whether
    -- the file has interior gaps (completion_kind='partial') or
    -- the recorder ended before the broadcast did (truncated).
    COUNT(*) FILTER (WHERE completion_kind = 'partial' OR truncated)::BIGINT AS incomplete,
    -- Distinct channels recorded — feeds the page subtitle. Folded
    -- into this aggregate so the videos route doesn't need a
    -- separate full-channels-list fetch just to surface a count.
    COUNT(DISTINCT broadcaster_id)::BIGINT AS channels
FROM videos WHERE deleted_at IS NULL;

-- name: StatisticsTotalsByBroadcaster :one
-- Per-channel rollup of finished recordings: count + summed bytes +
-- summed duration. Mirrors StatisticsTotals scoped to one broadcaster
-- so the watch page can render a "N recordings · X GB" line under the
-- channel name without paginating the full library client-side.
SELECT
    COUNT(*)::BIGINT AS total,
    COALESCE(SUM(size_bytes), 0)::BIGINT AS total_size,
    COALESCE(SUM(duration_seconds), 0)::DOUBLE PRECISION AS total_duration
FROM videos
WHERE broadcaster_id = $1 AND status = 'DONE' AND deleted_at IS NULL;
