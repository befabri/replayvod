-- name: GetCategory :one
SELECT * FROM categories WHERE id = $1;

-- name: GetCategoryByName :one
SELECT * FROM categories WHERE name = $1;

-- name: UpsertCategory :one
INSERT INTO categories (id, name, box_art_url, igdb_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    box_art_url = EXCLUDED.box_art_url,
    igdb_id = EXCLUDED.igdb_id,
    updated_at = NOW()
RETURNING *;

-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

-- name: ListCategoriesMissingBoxArt :many
SELECT * FROM categories WHERE box_art_url IS NULL OR box_art_url = '';
