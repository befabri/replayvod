-- name: GetChannel :one
SELECT * FROM channels WHERE broadcaster_id = ?;

-- name: GetChannelByLogin :one
SELECT * FROM channels WHERE broadcaster_login = ?;

-- name: UpsertChannel :one
INSERT INTO channels (
    broadcaster_id, broadcaster_login, broadcaster_name, broadcaster_language,
    profile_image_url, offline_image_url, description, broadcaster_type, view_count
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (broadcaster_id) DO UPDATE SET
    broadcaster_login = excluded.broadcaster_login,
    broadcaster_name = excluded.broadcaster_name,
    broadcaster_language = excluded.broadcaster_language,
    profile_image_url = excluded.profile_image_url,
    offline_image_url = excluded.offline_image_url,
    description = excluded.description,
    broadcaster_type = excluded.broadcaster_type,
    view_count = excluded.view_count,
    updated_at = datetime('now')
RETURNING *;

-- name: ListChannels :many
SELECT * FROM channels ORDER BY broadcaster_login;

-- name: ListChannelsByIDs :many
SELECT * FROM channels WHERE broadcaster_id IN (sqlc.slice('ids'));

-- NOTE: SearchChannels is hand-rolled in
-- internal/repository/sqliteadapter/channels.go for the same reason as
-- ListVideos (see queries/sqlite/videos.sql).

-- name: DeleteChannel :exec
DELETE FROM channels WHERE broadcaster_id = ?;

-- name: UpsertUserFollow :exec
INSERT INTO user_followed_channels (user_id, broadcaster_id, followed_at, followed)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id, broadcaster_id) DO UPDATE SET
    followed_at = excluded.followed_at,
    followed = excluded.followed;

-- name: ListUserFollows :many
SELECT c.* FROM channels c
INNER JOIN user_followed_channels ufc ON ufc.broadcaster_id = c.broadcaster_id
WHERE ufc.user_id = ? AND ufc.followed = 1
ORDER BY c.broadcaster_login;

-- name: UnfollowChannel :exec
UPDATE user_followed_channels SET followed = 0 WHERE user_id = ? AND broadcaster_id = ?;
