-- name: GetChannelUserState :one
SELECT * FROM channel_user_states WHERE user_id = $1 AND broadcaster_id = $2;

-- name: ListChannelUserStatesForChannels :many
SELECT * FROM channel_user_states
WHERE user_id = $1 AND broadcaster_id = ANY(@broadcaster_ids::text[]);

-- name: SetChannelFavorite :one
INSERT INTO channel_user_states (user_id, broadcaster_id, favorite, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT(user_id, broadcaster_id) DO UPDATE SET
    favorite = EXCLUDED.favorite,
    updated_at = NOW()
RETURNING *;
