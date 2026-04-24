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
-- completion_kind distinguishes clean-end from partial recordings.
-- Callers pass 'complete' for naturally-ended streams with no gaps,
-- 'partial' when resume_state contained restart_window_rolled gaps
-- (i.e. we lost data the CDN rolled past during shutdown).
UPDATE videos SET
    status = 'DONE',
    downloaded_at = NOW(),
    duration_seconds = $2,
    size_bytes = $3,
    thumbnail = $4,
    completion_kind = $5
WHERE id = $1;

-- name: MarkVideoFailed :exec
-- completion_kind is 'cancelled' when the operator called Cancel(),
-- 'complete' otherwise (the default; pipeline crashed / transport
-- exhausted / etc.). Downstream UI keys on this to render a grey
-- "CANCELLED" badge instead of red "FAILED" for user-initiated
-- stops.
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = NOW(),
    error = $2,
    completion_kind = $3
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

-- name: ListVideosByBroadcaster :many
SELECT * FROM videos
WHERE broadcaster_id = $1 AND deleted_at IS NULL
ORDER BY start_download_at DESC
LIMIT $2 OFFSET $3;

-- name: ListVideosByCategory :many
SELECT v.* FROM videos v
INNER JOIN video_categories vc ON vc.video_id = v.id
WHERE vc.category_id = $1 AND v.deleted_at IS NULL
ORDER BY v.start_download_at DESC
LIMIT $2 OFFSET $3;

-- name: ListVideosMissingThumbnail :many
SELECT * FROM videos WHERE status = 'DONE' AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: SoftDeleteVideo :exec
UPDATE videos SET deleted_at = NOW() WHERE id = $1;

-- name: CountVideosByStatus :one
SELECT COUNT(*) FROM videos WHERE status = $1 AND deleted_at IS NULL;

-- name: StatisticsByStatus :many
SELECT status, COUNT(*) AS count FROM videos WHERE deleted_at IS NULL GROUP BY status;

-- name: StatisticsTotals :one
SELECT
    COUNT(*)::BIGINT AS total,
    COALESCE(SUM(size_bytes), 0)::BIGINT AS total_size,
    COALESCE(SUM(duration_seconds), 0)::DOUBLE PRECISION AS total_duration
FROM videos WHERE status = 'DONE' AND deleted_at IS NULL;
