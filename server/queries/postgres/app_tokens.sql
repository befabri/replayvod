-- name: GetLatestAppToken :one
SELECT * FROM app_access_tokens WHERE expires_at > NOW() ORDER BY created_at DESC LIMIT 1;

-- name: CreateAppToken :one
INSERT INTO app_access_tokens (token, expires_at) VALUES ($1, $2) RETURNING *;

-- name: DeleteExpiredAppTokens :exec
DELETE FROM app_access_tokens WHERE expires_at < NOW();
