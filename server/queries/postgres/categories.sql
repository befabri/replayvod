-- name: GetCategory :one
SELECT * FROM categories WHERE id = $1;

-- name: GetCategoryByName :one
SELECT * FROM categories WHERE name = $1;

-- name: UpsertCategory :one
-- Preserves box_art_url and igdb_id on conflict: a webhook-path
-- upsert that only knows (id, name) won't wipe values the
-- category-art sync has filled. COALESCE picks the existing row
-- value when the caller passed NULL. UpdateCategoryBoxArt below is
-- the explicit path to actively change the art.
INSERT INTO categories (id, name, box_art_url, igdb_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    -- NULLIF normalizes an explicit empty-string payload to NULL
    -- before COALESCE decides, so a caller passing &"" can't wipe
    -- the existing art any more than a nil caller can.
    box_art_url = COALESCE(NULLIF(EXCLUDED.box_art_url, ''), categories.box_art_url),
    igdb_id = COALESCE(NULLIF(EXCLUDED.igdb_id, ''), categories.igdb_id),
    updated_at = NOW()
RETURNING *;

-- name: UpdateCategoryBoxArt :exec
-- Dedicated setter for box_art_url. Used by the category-art sync
-- task and the Hydrator's eager enrichment; separates "refresh just
-- the art" from the broader UpsertCategory contract.
UPDATE categories SET box_art_url = $2, updated_at = NOW() WHERE id = $1;

-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

-- name: SearchCategories :many
-- Case-insensitive substring match on name. Ranks exact name match
-- first, then prefix match, then substring match, then alphabetical.
-- Mirrors queries/postgres/channels.sql SearchChannels so both
-- combobox-backed dropdowns (schedule form channel picker + category
-- picker) share a ranking contract. Empty query returns everything
-- up to row_limit, so the same endpoint backs the "show all" state.
SELECT * FROM categories
WHERE @query::text = ''
   OR name ILIKE '%' || @query::text || '%'
ORDER BY
    CASE
        WHEN @query::text = '' THEN 3
        WHEN lower(name) = lower(@query::text) THEN 0
        WHEN lower(name) LIKE lower(@query::text) || '%' THEN 1
        ELSE 2
    END,
    name
LIMIT @row_limit;

-- name: ListCategoriesMissingBoxArt :many
SELECT * FROM categories WHERE box_art_url IS NULL OR box_art_url = '';
