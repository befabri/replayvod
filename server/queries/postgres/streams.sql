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

-- name: ListLatestLivePerChannel :many
-- Returns the most recent stream per broadcaster, newest first, joined
-- with the channel for display metadata. DISTINCT ON requires ordering
-- by its key first, so the inner query picks the latest per broadcaster
-- and the outer query re-sorts globally by started_at.
SELECT * FROM (
    SELECT DISTINCT ON (s.broadcaster_id)
        s.id,
        s.broadcaster_id,
        s.type,
        s.language,
        s.thumbnail_url,
        s.viewer_count,
        s.is_mature,
        s.started_at,
        s.ended_at,
        s.created_at,
        c.broadcaster_login,
        c.broadcaster_name,
        c.profile_image_url
    FROM streams s
    INNER JOIN channels c ON c.broadcaster_id = s.broadcaster_id
    ORDER BY s.broadcaster_id, s.started_at DESC
) latest
ORDER BY latest.started_at DESC
LIMIT $1;
