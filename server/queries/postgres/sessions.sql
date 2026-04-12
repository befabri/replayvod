-- name: CreateSession :exec
INSERT INTO sessions (hashed_id, user_id, encrypted_tokens, expires_at, user_agent, ip_address)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetSession :one
SELECT * FROM sessions WHERE hashed_id = $1;

-- name: UpdateSessionTokens :exec
UPDATE sessions SET encrypted_tokens = $2, last_active_at = NOW() WHERE hashed_id = $1;

-- name: UpdateSessionActivity :exec
UPDATE sessions SET last_active_at = NOW() WHERE hashed_id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE hashed_id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < NOW();

-- name: ListUserSessions :many
SELECT hashed_id, user_id, expires_at, last_active_at, user_agent, ip_address, created_at
FROM sessions WHERE user_id = $1 ORDER BY last_active_at DESC;

-- name: CountUserSessions :one
SELECT COUNT(*) FROM sessions WHERE user_id = $1;
