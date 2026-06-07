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

-- name: ListChannelsPageAsc :many
WITH params AS (
    SELECT CAST(@live_only AS integer) AS live_only,
           CAST(@downloaded_only AS integer) AS downloaded_only,
           CAST(@favorite_only AS integer) AS favorite_only,
           CAST(@user_id AS text) AS user_id,
           CAST(sqlc.narg('cursor_name') AS text) AS cursor_name,
           CAST(@cursor_id AS text) AS cursor_id,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT c.* FROM channels c
CROSS JOIN params
WHERE (
    params.live_only = 0
    OR EXISTS (
        SELECT 1 FROM streams s
        WHERE s.broadcaster_id = c.broadcaster_id AND s.ended_at IS NULL
    )
)
  AND (
    params.downloaded_only = 0
    OR EXISTS (
        SELECT 1 FROM videos v
        WHERE v.broadcaster_id = c.broadcaster_id
          AND v.status = 'DONE'
          AND v.deleted_at IS NULL
    )
)
  AND (
    params.favorite_only = 0
    OR EXISTS (
        SELECT 1 FROM channel_user_states cus
        WHERE cus.broadcaster_id = c.broadcaster_id
          AND cus.user_id = params.user_id
          AND cus.favorite = 1
    )
)
  AND (
    params.cursor_name IS NULL
    OR lower(c.broadcaster_name) > lower(params.cursor_name)
    OR (lower(c.broadcaster_name) = lower(params.cursor_name) AND c.broadcaster_id > params.cursor_id)
  )
ORDER BY lower(c.broadcaster_name) ASC, c.broadcaster_id ASC
LIMIT (SELECT row_limit FROM params);

-- name: ListChannelsPageDesc :many
WITH params AS (
    SELECT CAST(@live_only AS integer) AS live_only,
           CAST(@downloaded_only AS integer) AS downloaded_only,
           CAST(@favorite_only AS integer) AS favorite_only,
           CAST(@user_id AS text) AS user_id,
           CAST(sqlc.narg('cursor_name') AS text) AS cursor_name,
           CAST(@cursor_id AS text) AS cursor_id,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT c.* FROM channels c
CROSS JOIN params
WHERE (
    params.live_only = 0
    OR EXISTS (
        SELECT 1 FROM streams s
        WHERE s.broadcaster_id = c.broadcaster_id AND s.ended_at IS NULL
    )
)
  AND (
    params.downloaded_only = 0
    OR EXISTS (
        SELECT 1 FROM videos v
        WHERE v.broadcaster_id = c.broadcaster_id
          AND v.status = 'DONE'
          AND v.deleted_at IS NULL
    )
)
  AND (
    params.favorite_only = 0
    OR EXISTS (
        SELECT 1 FROM channel_user_states cus
        WHERE cus.broadcaster_id = c.broadcaster_id
          AND cus.user_id = params.user_id
          AND cus.favorite = 1
    )
)
  AND (
    params.cursor_name IS NULL
    OR lower(c.broadcaster_name) < lower(params.cursor_name)
    OR (lower(c.broadcaster_name) = lower(params.cursor_name) AND c.broadcaster_id < params.cursor_id)
  )
ORDER BY lower(c.broadcaster_name) DESC, c.broadcaster_id DESC
LIMIT (SELECT row_limit FROM params);

-- name: SearchChannels :many
-- Case-insensitive substring match on login + display name. Ranks exact
-- login match first, then prefix match, then substring match, then
-- alphabetical. Bind params once in a CTE with explicit casts so sqlc's
-- SQLite output stays typed through the repeated CASE/LIKE expressions.
WITH params AS (
    SELECT CAST(@query AS text) AS search_query,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT c.* FROM channels c
CROSS JOIN params
WHERE params.search_query = ''
   OR lower(c.broadcaster_login) LIKE '%' || lower(params.search_query) || '%'
   OR lower(c.broadcaster_name)  LIKE '%' || lower(params.search_query) || '%'
ORDER BY
    CASE
        WHEN params.search_query = '' THEN 3
        WHEN lower(c.broadcaster_login) = lower(params.search_query) THEN 0
        WHEN lower(c.broadcaster_login) LIKE lower(params.search_query) || '%' THEN 1
        WHEN lower(c.broadcaster_name)  LIKE lower(params.search_query) || '%' THEN 1
        ELSE 2
    END,
    c.broadcaster_login
LIMIT (SELECT row_limit FROM params);

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
