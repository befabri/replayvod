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
-- See postgres/videos.sql MarkVideoDone for completion_kind rationale.
UPDATE videos SET
    status = 'DONE',
    downloaded_at = datetime('now'),
    duration_seconds = ?,
    size_bytes = ?,
    thumbnail = ?,
    completion_kind = ?
WHERE id = ?;

-- name: MarkVideoFailed :exec
-- See postgres/videos.sql MarkVideoFailed for completion_kind rationale.
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = datetime('now'),
    error = ?,
    completion_kind = ?
WHERE id = ?;

-- name: SetVideoThumbnail :exec
UPDATE videos SET thumbnail = ? WHERE id = ?;

-- NOTE: ListVideos is intentionally NOT declared here. The PG path
-- uses a CASE-based dynamic ORDER BY (see queries/postgres/videos.sql),
-- but sqlc's SQLite engine can't infer the param type of a named arg
-- referenced only inside CASE expressions, so the equivalent query is
-- hand-rolled against the raw *sql.DB in
-- internal/repository/sqliteadapter/videos.go.

-- name: ListVideosByBroadcaster :many
SELECT * FROM videos
WHERE broadcaster_id = ? AND deleted_at IS NULL
ORDER BY start_download_at DESC
LIMIT ? OFFSET ?;

-- name: ListVideosByCategory :many
SELECT v.* FROM videos v
INNER JOIN video_categories vc ON vc.video_id = v.id
WHERE vc.category_id = ? AND v.deleted_at IS NULL
ORDER BY v.start_download_at DESC
LIMIT ? OFFSET ?;

-- name: ListVideosMissingThumbnail :many
SELECT * FROM videos WHERE status = 'DONE' AND thumbnail IS NULL AND deleted_at IS NULL;

-- name: SoftDeleteVideo :exec
UPDATE videos SET deleted_at = datetime('now') WHERE id = ?;

-- name: CountVideosByStatus :one
SELECT COUNT(*) FROM videos WHERE status = ? AND deleted_at IS NULL;

-- name: StatisticsByStatus :many
SELECT status, COUNT(*) AS count FROM videos WHERE deleted_at IS NULL GROUP BY status;

-- name: StatisticsTotals :one
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(size_bytes), 0) AS INTEGER) AS total_size,
    CAST(COALESCE(SUM(duration_seconds), 0) AS REAL) AS total_duration
FROM videos WHERE status = 'DONE' AND deleted_at IS NULL;
