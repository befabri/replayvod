-- name: GetVideoPlaybackAsset :one
SELECT * FROM video_playback_assets WHERE video_id = ?;

-- name: UpsertVideoPlaybackAsset :one
INSERT INTO video_playback_assets (
    video_id, status, filename, mime_type,
    duration_seconds, size_bytes, error, generated_at, last_accessed_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(video_id) DO UPDATE SET
    status = excluded.status,
    filename = excluded.filename,
    mime_type = excluded.mime_type,
    duration_seconds = excluded.duration_seconds,
    size_bytes = excluded.size_bytes,
    error = excluded.error,
    generated_at = excluded.generated_at,
    last_accessed_at = excluded.last_accessed_at,
    updated_at = datetime('now')
RETURNING *;

-- name: TouchVideoPlaybackAsset :exec
UPDATE video_playback_assets
SET last_accessed_at = datetime('now'),
    updated_at = datetime('now')
WHERE video_id = ?
  AND status = 'ready';

-- name: ListReadyVideoPlaybackAssets :many
SELECT * FROM video_playback_assets WHERE status='ready'
 AND (last_accessed_at,generated_at,video_id)
 > (sqlc.arg(after_access),sqlc.arg(after_generated),CAST(sqlc.arg(after_id) AS BIGINT))
ORDER BY last_accessed_at,generated_at,video_id LIMIT sqlc.arg(limit);

-- name: SumReadyPlaybackBytes :one
SELECT CAST(coalesce(sum(size_bytes),0) AS BIGINT) FROM video_playback_assets WHERE status='ready';

-- name: DeleteVideoPlaybackAsset :exec
DELETE FROM video_playback_assets WHERE video_id = ?;
