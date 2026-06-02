-- name: GetVideoPlaybackAsset :one
SELECT * FROM video_playback_assets WHERE video_id = $1;

-- name: UpsertVideoPlaybackAsset :one
INSERT INTO video_playback_assets (
    video_id, status, filename, mime_type,
    duration_seconds, size_bytes, error, generated_at, last_accessed_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT(video_id) DO UPDATE SET
    status = excluded.status,
    filename = excluded.filename,
    mime_type = excluded.mime_type,
    duration_seconds = excluded.duration_seconds,
    size_bytes = excluded.size_bytes,
    error = excluded.error,
    generated_at = excluded.generated_at,
    last_accessed_at = excluded.last_accessed_at,
    updated_at = NOW()
RETURNING *;

-- name: TouchVideoPlaybackAsset :exec
UPDATE video_playback_assets
SET last_accessed_at = NOW(),
    updated_at = NOW()
WHERE video_id = $1
  AND status = 'ready';

-- name: ListReadyVideoPlaybackAssets :many
SELECT *
FROM video_playback_assets
WHERE status = 'ready'
ORDER BY last_accessed_at ASC, generated_at ASC, video_id ASC;

-- name: DeleteVideoPlaybackAsset :exec
DELETE FROM video_playback_assets WHERE video_id = $1;
