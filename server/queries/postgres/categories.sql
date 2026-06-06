-- name: GetCategory :one
SELECT * FROM categories WHERE id = $1;

-- name: GetCategoryDetail :one
SELECT
    c.id,
    c.name,
    c.box_art_url,
    c.igdb_id,
    c.description,
    c.game_metadata_checked_at,
    c.description_checked_at,
    c.created_at,
    c.updated_at,
    COUNT(v.id)::BIGINT AS video_count,
    COALESCE(SUM(v.size_bytes), 0)::BIGINT AS total_size
FROM categories c
LEFT JOIN video_categories vc ON vc.category_id = c.id
LEFT JOIN videos v ON v.id = vc.video_id AND v.deleted_at IS NULL
WHERE c.id = $1
GROUP BY c.id, c.name, c.box_art_url, c.igdb_id, c.description, c.game_metadata_checked_at, c.description_checked_at, c.created_at, c.updated_at;

-- name: GetCategoryByName :one
SELECT * FROM categories WHERE name = $1;

-- name: UpsertCategory :one
-- Preserves box_art_url, igdb_id, and description on ordinary webhook-path
-- upserts that only know (id, name). When a non-empty incoming igdb_id changes
-- the mapped IGDB game, the existing description cache is cleared so it can be
-- re-enriched for the new game. UpdateCategoryGameMetadata below is the
-- explicit path to actively change Twitch game metadata.
INSERT INTO categories (id, name, box_art_url, igdb_id, description)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    -- NULLIF normalizes an explicit empty-string payload to NULL
    -- before COALESCE decides, so a caller passing &"" can't wipe
    -- the existing art any more than a nil caller can.
    box_art_url = COALESCE(NULLIF(EXCLUDED.box_art_url, ''), categories.box_art_url),
    igdb_id = COALESCE(NULLIF(EXCLUDED.igdb_id, ''), categories.igdb_id),
    description = CASE
        WHEN NULLIF(EXCLUDED.description, '') IS NOT NULL THEN EXCLUDED.description
        WHEN NULLIF(EXCLUDED.igdb_id, '') IS NOT NULL
             AND NULLIF(EXCLUDED.igdb_id, '') IS DISTINCT FROM categories.igdb_id
        THEN NULL
        ELSE categories.description
    END,
    description_checked_at = CASE
        WHEN NULLIF(EXCLUDED.igdb_id, '') IS NOT NULL
             AND NULLIF(EXCLUDED.igdb_id, '') IS DISTINCT FROM categories.igdb_id
        THEN NULL
        ELSE categories.description_checked_at
    END,
    updated_at = NOW()
RETURNING *;

-- name: UpdateCategoryGameMetadata :exec
-- Refresh the Twitch-side category metadata returned by Helix /games.
-- Empty inputs preserve the existing value so callers can safely write
-- whichever subset Twitch returned.
UPDATE categories
SET box_art_url = COALESCE(NULLIF($2, ''), box_art_url),
    igdb_id = COALESCE(NULLIF($3, ''), igdb_id),
    description = CASE
        WHEN NULLIF($3, '') IS NOT NULL AND NULLIF($3, '') IS DISTINCT FROM igdb_id
        THEN NULL
        ELSE description
    END,
    description_checked_at = CASE
        WHEN NULLIF($3, '') IS NOT NULL AND NULLIF($3, '') IS DISTINCT FROM igdb_id
        THEN NULL
        ELSE description_checked_at
    END,
    game_metadata_checked_at = NOW(),
    updated_at = NOW()
WHERE id = $1;

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

-- name: ListCategoriesMissingGameMetadata :many
-- Helix /games is the source for both box_art_url and igdb_id. Keep this
-- broader than the historical box-art-only query so categories that already
-- have art but still lack igdb_id can become eligible for IGDB enrichment.
SELECT * FROM categories
WHERE (box_art_url IS NULL OR box_art_url = ''
       OR igdb_id IS NULL OR igdb_id = '')
  AND (game_metadata_checked_at IS NULL OR game_metadata_checked_at < $1)
ORDER BY name;

-- name: MarkCategoryGameMetadataChecked :exec
UPDATE categories
SET game_metadata_checked_at = NOW(),
    updated_at = NOW()
WHERE id = $1;

-- name: ListCategoriesMissingDescription :many
-- IGDB descriptions need a numeric igdb_id. Rows without one are left to the
-- Helix metadata sync first.
SELECT * FROM categories
WHERE igdb_id IS NOT NULL AND igdb_id <> ''
  AND (description IS NULL OR description = '')
  AND (description_checked_at IS NULL OR description_checked_at < $1)
ORDER BY name;

-- name: UpdateCategoryDescription :exec
UPDATE categories
SET description = $2,
    description_checked_at = NOW(),
    updated_at = NOW()
WHERE id = $1;

-- name: MarkCategoryDescriptionChecked :exec
UPDATE categories
SET description_checked_at = NOW(),
    updated_at = NOW()
WHERE id = $1;

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
