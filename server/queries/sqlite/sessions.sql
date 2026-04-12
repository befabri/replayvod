-- name: CreateSession :exec
INSERT INTO sessions (hashed_id, user_id, encrypted_tokens, expires_at, user_agent, ip_address)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE hashed_id = ?;

-- name: UpdateSessionTokens :exec
UPDATE sessions SET encrypted_tokens = ?, last_active_at = datetime('now') WHERE hashed_id = ?;

-- name: UpdateSessionActivity :exec
UPDATE sessions SET last_active_at = datetime('now') WHERE hashed_id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE hashed_id = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < datetime('now');

-- name: ListUserSessions :many
SELECT hashed_id, user_id, expires_at, last_active_at, user_agent, ip_address, created_at
FROM sessions WHERE user_id = ? ORDER BY last_active_at DESC;

-- name: CountUserSessions :one
SELECT COUNT(*) FROM sessions WHERE user_id = ?;
