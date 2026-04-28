-- name: GetVideo :one
SELECT * FROM videos WHERE id = ?;

-- name: GetVideoByJobID :one
SELECT * FROM videos WHERE job_id = ?;

-- name: CreateVideo :one
INSERT INTO videos (
    job_id, filename, display_name, title, status, quality,
    broadcaster_id, stream_id, viewer_count, language, recording_type,
    force_h264
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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

-- NOTE: ListVideos is intentionally NOT declared here. The PG path
-- uses a CASE-based dynamic ORDER BY (see queries/postgres/videos.sql),
-- but sqlc's SQLite engine can't infer the param type of a named arg
-- referenced only inside CASE expressions, so the equivalent query is
-- hand-rolled against the raw *sql.DB in
-- internal/repository/sqliteadapter/videos.go.

-- name: ListVideosMissingThumbnail :many
SELECT * FROM videos WHERE status = 'DONE' AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: SoftDeleteVideo :exec
UPDATE videos SET deleted_at = datetime('now') WHERE id = ?;

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
