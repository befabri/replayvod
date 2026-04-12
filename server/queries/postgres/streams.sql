-- name: GetStream :one
SELECT * FROM streams WHERE id = $1;

-- name: UpsertStream :one
INSERT INTO streams (
    id, broadcaster_id, type, language, thumbnail_url,
    viewer_count, is_mature, started_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    type = EXCLUDED.type,
    language = EXCLUDED.language,
    thumbnail_url = EXCLUDED.thumbnail_url,
    viewer_count = EXCLUDED.viewer_count,
    is_mature = EXCLUDED.is_mature
RETURNING *;

-- name: EndStream :exec
UPDATE streams SET ended_at = $2 WHERE id = $1 AND ended_at IS NULL;

-- name: UpdateStreamViewers :exec
UPDATE streams SET viewer_count = $2 WHERE id = $1;

-- name: ListActiveStreams :many
SELECT * FROM streams WHERE ended_at IS NULL ORDER BY started_at DESC;

-- name: ListStreamsByBroadcaster :many
SELECT * FROM streams WHERE broadcaster_id = $1 ORDER BY started_at DESC LIMIT $2 OFFSET $3;

-- name: GetLastLiveStream :one
SELECT * FROM streams WHERE broadcaster_id = $1 ORDER BY started_at DESC LIMIT 1;
