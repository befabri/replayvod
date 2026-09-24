-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserForUpdate :one
SELECT * FROM users WHERE id = $1 FOR UPDATE;

-- name: UpsertUser :one
INSERT INTO users (id, login, display_name, email, profile_image_url, role)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    login = EXCLUDED.login,
    display_name = EXCLUDED.display_name,
    email = EXCLUDED.email,
    profile_image_url = EXCLUDED.profile_image_url,
    updated_at = NOW()
RETURNING *;

-- name: ReleaseUserLogin :exec
-- Twitch hands a renamed account's old login to someone else, so another row
-- still holding it is stale. Park it on a placeholder no Twitch login can
-- take; that account's next sign-in restores its current login.
UPDATE users SET login = '~' || id, updated_at = NOW() WHERE login = $1 AND id <> $2;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: UpdateUserRole :exec
UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1;

-- name: ListUserDisplayNames :many
SELECT id, display_name FROM users WHERE id = ANY($1::text[]);
