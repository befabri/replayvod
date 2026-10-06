-- name: UpsertTag :one
INSERT INTO tags (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListTags :many
-- Use bytewise ordering on every host, matching SQLite's BINARY collation.
SELECT * FROM tags ORDER BY name COLLATE "C";
