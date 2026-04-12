-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByLogin :one
SELECT * FROM users WHERE login = ?;

-- name: UpsertUser :one
INSERT INTO users (id, login, display_name, email, profile_image_url, role)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    login = excluded.login,
    display_name = excluded.display_name,
    email = excluded.email,
    profile_image_url = excluded.profile_image_url,
    updated_at = datetime('now')
RETURNING *;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: UpdateUserRole :exec
UPDATE users SET role = ?, updated_at = datetime('now') WHERE id = ?;
