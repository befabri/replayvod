-- name: GetCategory :one
SELECT * FROM categories WHERE id = ?;

-- name: GetCategoryByName :one
SELECT * FROM categories WHERE name = ?;

-- name: UpsertCategory :one
-- Preserves box_art_url and igdb_id on conflict: a webhook-path
-- upsert that only knows (id, name) won't wipe values the category-
-- art sync has filled. ifnull() picks the existing row value when
-- the caller passed NULL.
INSERT INTO categories (id, name, box_art_url, igdb_id)
VALUES (?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name,
    -- nullif normalizes an explicit empty-string payload to NULL
    -- before ifnull decides, so a caller passing &"" can't wipe
    -- the existing art any more than a nil caller can.
    box_art_url = ifnull(nullif(excluded.box_art_url, ''), categories.box_art_url),
    igdb_id = ifnull(nullif(excluded.igdb_id, ''), categories.igdb_id),
    updated_at = datetime('now')
RETURNING *;

-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

-- name: ListCategoriesMissingBoxArt :many
SELECT * FROM categories WHERE box_art_url IS NULL OR box_art_url = '';

-- name: UpdateCategoryBoxArt :exec
-- Dedicated setter for box_art_url, the explicit "refresh art" path
-- used by categoryart.Service (both the eager Hydrator enrichment
-- and the scheduled backfill task). Separating this from
-- UpsertCategory keeps "update art" distinct from "upsert name".
-- Positional ? rather than named @ because sqlc's SQLite rewriter
-- has been flaky combining named params with SET clauses in UPDATE
-- (observed as tokens swallowing adjacent characters on generate).
UPDATE categories SET box_art_url = ?, updated_at = datetime('now') WHERE id = ?;
