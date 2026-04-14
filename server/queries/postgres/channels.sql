-- name: GetChannel :one
SELECT * FROM channels WHERE broadcaster_id = $1;

-- name: GetChannelByLogin :one
SELECT * FROM channels WHERE broadcaster_login = $1;

-- name: UpsertChannel :one
INSERT INTO channels (
    broadcaster_id, broadcaster_login, broadcaster_name, broadcaster_language,
    profile_image_url, offline_image_url, description, broadcaster_type, view_count
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (broadcaster_id) DO UPDATE SET
    broadcaster_login = EXCLUDED.broadcaster_login,
    broadcaster_name = EXCLUDED.broadcaster_name,
    broadcaster_language = EXCLUDED.broadcaster_language,
    profile_image_url = EXCLUDED.profile_image_url,
    offline_image_url = EXCLUDED.offline_image_url,
    description = EXCLUDED.description,
    broadcaster_type = EXCLUDED.broadcaster_type,
    view_count = EXCLUDED.view_count,
    updated_at = NOW()
RETURNING *;

-- name: ListChannels :many
SELECT * FROM channels ORDER BY broadcaster_login;

-- name: ListChannelsByIDs :many
SELECT * FROM channels WHERE broadcaster_id = ANY(@ids::text[]);

-- name: SearchChannels :many
-- Case-insensitive substring match on login + display name. Ranks exact
-- login match first, then prefix match, then substring match, then
-- alphabetical — so typing "sho" surfaces "shroud" before "ashotoftoast".
-- Empty query returns everything (up to row_limit), so the same endpoint
-- backs the "show all" state of a combobox without a second query.
SELECT * FROM channels
WHERE @query::text = ''
   OR broadcaster_login ILIKE '%' || @query::text || '%'
   OR broadcaster_name  ILIKE '%' || @query::text || '%'
ORDER BY
    CASE
        WHEN @query::text = '' THEN 3
        WHEN lower(broadcaster_login) = lower(@query::text) THEN 0
        WHEN lower(broadcaster_login) LIKE lower(@query::text) || '%' THEN 1
        WHEN lower(broadcaster_name)  LIKE lower(@query::text) || '%' THEN 1
        ELSE 2
    END,
    broadcaster_login
LIMIT @row_limit;

-- name: DeleteChannel :exec
DELETE FROM channels WHERE broadcaster_id = $1;

-- name: UpsertUserFollow :exec
INSERT INTO user_followed_channels (user_id, broadcaster_id, followed_at, followed)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, broadcaster_id) DO UPDATE SET
    followed_at = EXCLUDED.followed_at,
    followed = EXCLUDED.followed;

-- name: ListUserFollows :many
SELECT c.* FROM channels c
INNER JOIN user_followed_channels ufc ON ufc.broadcaster_id = c.broadcaster_id
WHERE ufc.user_id = $1 AND ufc.followed = TRUE
ORDER BY c.broadcaster_login;

-- name: UnfollowChannel :exec
UPDATE user_followed_channels SET followed = FALSE WHERE user_id = $1 AND broadcaster_id = $2;
