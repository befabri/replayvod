-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByLogin :one
SELECT * FROM users WHERE login = $1;

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

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: UpdateUserRole :exec
UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1;
