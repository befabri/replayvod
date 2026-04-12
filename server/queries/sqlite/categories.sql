-- name: GetCategory :one
SELECT * FROM categories WHERE id = ?;

-- name: GetCategoryByName :one
SELECT * FROM categories WHERE name = ?;

-- name: UpsertCategory :one
INSERT INTO categories (id, name, box_art_url, igdb_id)
VALUES (?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name,
    box_art_url = excluded.box_art_url,
    igdb_id = excluded.igdb_id,
    updated_at = datetime('now')
RETURNING *;

-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

-- name: ListCategoriesMissingBoxArt :many
SELECT * FROM categories WHERE box_art_url IS NULL OR box_art_url = '';
