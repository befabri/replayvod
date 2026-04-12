-- name: IsWhitelisted :one
SELECT COUNT(*) > 0 FROM whitelist WHERE twitch_user_id = ?;

-- name: AddToWhitelist :exec
INSERT OR IGNORE INTO whitelist (twitch_user_id) VALUES (?);

-- name: RemoveFromWhitelist :exec
DELETE FROM whitelist WHERE twitch_user_id = ?;

-- name: ListWhitelist :many
SELECT * FROM whitelist ORDER BY added_at DESC;
