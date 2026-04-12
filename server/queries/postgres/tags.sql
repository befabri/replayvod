-- name: GetTag :one
SELECT * FROM tags WHERE id = $1;

-- name: GetTagByName :one
SELECT * FROM tags WHERE name = $1;

-- name: UpsertTag :one
INSERT INTO tags (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListTags :many
SELECT * FROM tags ORDER BY name;
