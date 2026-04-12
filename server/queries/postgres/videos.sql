-- name: GetVideo :one
SELECT * FROM videos WHERE id = $1;

-- name: GetVideoByJobID :one
SELECT * FROM videos WHERE job_id = $1;

-- name: CreateVideo :one
INSERT INTO videos (
    job_id, filename, display_name, status, quality,
    broadcaster_id, stream_id, viewer_count, language, recording_type,
    force_h264
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateVideoStatus :exec
UPDATE videos SET status = $2 WHERE id = $1;

-- name: MarkVideoDone :exec
UPDATE videos SET
    status = 'DONE',
    downloaded_at = NOW(),
    duration_seconds = $2,
    size_bytes = $3,
    thumbnail = $4
WHERE id = $1;

-- name: MarkVideoFailed :exec
UPDATE videos SET
    status = 'FAILED',
    downloaded_at = NOW(),
    error = $2
WHERE id = $1;

-- name: SetVideoThumbnail :exec
UPDATE videos SET thumbnail = $2 WHERE id = $1;

-- name: ListVideos :many
SELECT * FROM videos WHERE deleted_at IS NULL ORDER BY start_download_at DESC LIMIT $1 OFFSET $2;

-- name: ListVideosByStatus :many
SELECT * FROM videos WHERE status = $1 AND deleted_at IS NULL ORDER BY start_download_at DESC LIMIT $2 OFFSET $3;

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
