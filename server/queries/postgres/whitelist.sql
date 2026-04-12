-- name: IsWhitelisted :one
SELECT EXISTS(SELECT 1 FROM whitelist WHERE twitch_user_id = $1);

-- name: AddToWhitelist :exec
INSERT INTO whitelist (twitch_user_id) VALUES ($1) ON CONFLICT DO NOTHING;

-- name: RemoveFromWhitelist :exec
DELETE FROM whitelist WHERE twitch_user_id = $1;

-- name: ListWhitelist :many
SELECT * FROM whitelist ORDER BY added_at DESC;
