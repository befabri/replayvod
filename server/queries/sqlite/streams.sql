-- name: GetStream :one
SELECT * FROM streams WHERE id = ?;

-- name: UpsertStream :one
INSERT INTO streams (
    id, broadcaster_id, type, language, thumbnail_url,
    viewer_count, is_mature, started_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    type = excluded.type,
    language = excluded.language,
    thumbnail_url = excluded.thumbnail_url,
    viewer_count = excluded.viewer_count,
    is_mature = excluded.is_mature
RETURNING *;

-- name: EndStream :exec
UPDATE streams SET ended_at = ? WHERE id = ? AND ended_at IS NULL;

-- name: UpdateStreamViewers :exec
UPDATE streams SET viewer_count = ? WHERE id = ?;

-- name: ListActiveStreams :many
SELECT * FROM streams WHERE ended_at IS NULL ORDER BY started_at DESC;

-- name: ListStreamsByBroadcaster :many
SELECT * FROM streams WHERE broadcaster_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?;

-- name: GetLastLiveStream :one
SELECT * FROM streams WHERE broadcaster_id = ? ORDER BY started_at DESC LIMIT 1;
