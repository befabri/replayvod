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

-- name: ListCategoriesWithVideos :many
-- Browse/library list: categories must be linked to at least one visible
-- recording. Twitch search can mirror catalog-only rows into categories; those
-- should stay out of the category page until a video actually references them.
SELECT c.* FROM categories c
WHERE EXISTS (
    SELECT 1
    FROM video_categories vc
    INNER JOIN videos v ON v.id = vc.video_id
    WHERE vc.category_id = c.id
      AND v.deleted_at IS NULL
)
ORDER BY c.name;

-- name: ListCategoriesWithVideosPageNameAsc :many
-- Cursor-paginated browse list for the default category order. This mirrors
-- ListCategoriesWithVideos' visibility predicate while over-fetching at the
-- adapter layer to discover the next cursor.
SELECT c.* FROM categories c
WHERE EXISTS (
    SELECT 1
    FROM video_categories vc
    INNER JOIN videos v ON v.id = vc.video_id
    WHERE vc.category_id = c.id
      AND v.deleted_at IS NULL
)
  AND (
    sqlc.narg('cursor_name')::text IS NULL
    OR lower(c.name) > lower(sqlc.narg('cursor_name')::text)
    OR (lower(c.name) = lower(sqlc.narg('cursor_name')::text) AND c.id > @cursor_id::text)
  )
ORDER BY lower(c.name) ASC, c.id ASC
LIMIT @row_limit;

-- name: ListCategoriesWithVideosPageLatestDesc :many
WITH category_stats AS (
    SELECT
        vc.category_id,
        MAX(v.start_download_at) AS latest_video_at,
        COUNT(DISTINCT vc.video_id)::bigint AS video_count
    FROM video_categories vc
    INNER JOIN videos v ON v.id = vc.video_id
    WHERE v.deleted_at IS NULL
    GROUP BY vc.category_id
)
SELECT c.*, category_stats.latest_video_at, category_stats.video_count
FROM categories c
INNER JOIN category_stats ON category_stats.category_id = c.id
WHERE (
    sqlc.narg('cursor_latest_video_at')::timestamptz IS NULL
    OR category_stats.latest_video_at < sqlc.narg('cursor_latest_video_at')::timestamptz
    OR (
        category_stats.latest_video_at = sqlc.narg('cursor_latest_video_at')::timestamptz
        AND lower(c.name) > lower(sqlc.narg('cursor_name')::text)
    )
    OR (
        category_stats.latest_video_at = sqlc.narg('cursor_latest_video_at')::timestamptz
        AND lower(c.name) = lower(sqlc.narg('cursor_name')::text)
        AND c.id > @cursor_id::text
    )
)
ORDER BY category_stats.latest_video_at DESC, lower(c.name) ASC, c.id ASC
LIMIT @row_limit;

-- name: ListCategoriesWithVideosPageVideoCountDesc :many
WITH category_stats AS (
    SELECT
        vc.category_id,
        MAX(v.start_download_at) AS latest_video_at,
        COUNT(DISTINCT vc.video_id)::bigint AS video_count
    FROM video_categories vc
    INNER JOIN videos v ON v.id = vc.video_id
    WHERE v.deleted_at IS NULL
    GROUP BY vc.category_id
)
SELECT c.*, category_stats.latest_video_at, category_stats.video_count
FROM categories c
INNER JOIN category_stats ON category_stats.category_id = c.id
WHERE (
    sqlc.narg('cursor_name')::text IS NULL
    OR category_stats.video_count < @cursor_video_count::bigint
    OR (
        category_stats.video_count = @cursor_video_count::bigint
        AND lower(c.name) > lower(sqlc.narg('cursor_name')::text)
    )
    OR (
        category_stats.video_count = @cursor_video_count::bigint
        AND lower(c.name) = lower(sqlc.narg('cursor_name')::text)
        AND c.id > @cursor_id::text
    )
)
ORDER BY category_stats.video_count DESC, lower(c.name) ASC, c.id ASC
LIMIT @row_limit;

-- name: ListCategoriesByIDs :many
SELECT * FROM categories WHERE id = ANY(@ids::text[]);

-- name: SearchCategories :many
-- Case-insensitive substring match on name. Ranks exact name match
-- first, then prefix match, then substring match, then alphabetical.
-- Mirrors queries/postgres/channels.sql SearchChannels so both
-- combobox-backed dropdowns (schedule form channel picker + category
-- picker) share a ranking contract. Empty query returns everything
-- up to row_limit, so the same endpoint backs the "show all" state.
SELECT * FROM categories
WHERE @query::text = ''
   OR lower(name) LIKE '%' || lower(@query::text) || '%'
ORDER BY
    CASE
        WHEN @query::text = '' THEN 3
        WHEN lower(name) = lower(@query::text) THEN 0
        WHEN lower(name) LIKE lower(@query::text) || '%' THEN 1
        ELSE 2
    END,
    name
LIMIT @row_limit;

-- name: SearchCategoriesWithVideos :many
-- Same ranking contract as SearchCategories, restricted to categories linked to
-- at least one visible recording.
SELECT c.* FROM categories c
WHERE (@query::text = ''
       OR lower(c.name) LIKE '%' || lower(@query::text) || '%')
  AND EXISTS (
      SELECT 1
      FROM video_categories vc
      INNER JOIN videos v ON v.id = vc.video_id
      WHERE vc.category_id = c.id
        AND v.deleted_at IS NULL
  )
ORDER BY
    CASE
        WHEN @query::text = '' THEN 3
        WHEN lower(c.name) = lower(@query::text) THEN 0
        WHEN lower(c.name) LIKE lower(@query::text) || '%' THEN 1
        ELSE 2
    END,
    c.name
LIMIT @row_limit;

-- name: ListCategoriesMissingBoxArt :many
SELECT * FROM categories WHERE box_art_url IS NULL OR box_art_url = '';

-- name: GetCategorySearchCache :one
SELECT * FROM category_search_cache WHERE normalized_query = $1;

-- name: UpsertCategorySearchCache :one
INSERT INTO category_search_cache (normalized_query, category_ids, expires_at, last_accessed_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (normalized_query) DO UPDATE SET
    category_ids = EXCLUDED.category_ids,
    expires_at = EXCLUDED.expires_at,
    last_accessed_at = EXCLUDED.last_accessed_at,
    updated_at = NOW()
RETURNING *;

-- name: TouchCategorySearchCache :exec
UPDATE category_search_cache
SET last_accessed_at = $2,
    updated_at = NOW()
WHERE normalized_query = $1;

-- name: DeleteExpiredCategorySearchCache :exec
DELETE FROM category_search_cache WHERE expires_at < $1;

-- name: PruneCategorySearchCache :exec
DELETE FROM category_search_cache
WHERE normalized_query IN (
    SELECT normalized_query
    FROM category_search_cache
    ORDER BY last_accessed_at DESC, updated_at DESC, normalized_query ASC
    OFFSET $1
);
