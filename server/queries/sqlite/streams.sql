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

-- name: ListLatestLivePerChannel :many
-- SQLite has no DISTINCT ON; use ROW_NUMBER() to pick the most recent
-- stream per broadcaster, then filter to rn=1. Joined with channels so
-- the caller gets display metadata in one round-trip.
SELECT
    id,
    broadcaster_id,
    type,
    language,
    thumbnail_url,
    viewer_count,
    is_mature,
    started_at,
    ended_at,
    created_at,
    broadcaster_login,
    broadcaster_name,
    profile_image_url
FROM (
    SELECT
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
        c.profile_image_url,
        ROW_NUMBER() OVER (
            PARTITION BY s.broadcaster_id
            ORDER BY s.started_at DESC
        ) AS rn
    FROM streams s
    INNER JOIN channels c ON c.broadcaster_id = s.broadcaster_id
) ranked
WHERE rn = 1
ORDER BY started_at DESC
LIMIT ?;
