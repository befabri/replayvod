-- name: GetChannelUserState :one
SELECT * FROM channel_user_states WHERE user_id = ? AND broadcaster_id = ?;

-- name: ListChannelUserStatesForChannels :many
SELECT * FROM channel_user_states
WHERE user_id = ? AND broadcaster_id IN (sqlc.slice('broadcaster_ids'));

-- name: SetChannelFavorite :one
INSERT INTO channel_user_states (user_id, broadcaster_id, favorite, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(user_id, broadcaster_id) DO UPDATE SET
    favorite = excluded.favorite,
    updated_at = datetime('now')
RETURNING *;
