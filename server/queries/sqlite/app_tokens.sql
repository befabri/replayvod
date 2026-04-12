-- name: GetLatestAppToken :one
SELECT * FROM app_access_tokens WHERE expires_at > datetime('now') ORDER BY created_at DESC LIMIT 1;

-- name: CreateAppToken :one
INSERT INTO app_access_tokens (token, expires_at) VALUES (?, ?) RETURNING *;

-- name: DeleteExpiredAppTokens :exec
DELETE FROM app_access_tokens WHERE expires_at < datetime('now');
