-- name: GetCategory :one
SELECT * FROM categories WHERE id = ?;

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
    CAST(COUNT(v.id) AS INTEGER) AS video_count,
    CAST(COALESCE(SUM(v.size_bytes), 0) AS INTEGER) AS total_size
FROM categories c
LEFT JOIN video_categories vc ON vc.category_id = c.id
LEFT JOIN videos v ON v.id = vc.video_id AND v.deleted_at IS NULL
WHERE c.id = ?
GROUP BY c.id, c.name, c.box_art_url, c.igdb_id, c.description, c.game_metadata_checked_at, c.description_checked_at, c.created_at, c.updated_at;

-- name: GetCategoryByName :one
SELECT * FROM categories WHERE name = ?;

-- name: UpsertCategory :one
-- Preserves box_art_url, igdb_id, and description on ordinary webhook-path
-- upserts that only know (id, name). When a non-empty incoming igdb_id changes
-- the mapped IGDB game, the existing description cache is cleared so it can be
-- re-enriched for the new game.
INSERT INTO categories (id, name, box_art_url, igdb_id, description)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name,
    -- nullif normalizes an explicit empty-string payload to NULL
    -- before ifnull decides, so a caller passing &"" can't wipe
    -- the existing art any more than a nil caller can.
    box_art_url = ifnull(nullif(excluded.box_art_url, ''), categories.box_art_url),
    igdb_id = ifnull(nullif(excluded.igdb_id, ''), categories.igdb_id),
    description = CASE
        WHEN nullif(excluded.description, '') IS NOT NULL THEN excluded.description
        WHEN nullif(excluded.igdb_id, '') IS NOT NULL
             AND ifnull(categories.igdb_id, '') <> nullif(excluded.igdb_id, '')
        THEN NULL
        ELSE categories.description
    END,
    description_checked_at = CASE
        WHEN nullif(excluded.igdb_id, '') IS NOT NULL
             AND ifnull(categories.igdb_id, '') <> nullif(excluded.igdb_id, '')
        THEN NULL
        ELSE categories.description_checked_at
    END,
    updated_at = datetime('now')
RETURNING *;

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
WITH params AS (
    SELECT CAST(sqlc.narg('cursor_name') AS text) AS cursor_name,
           CAST(@cursor_id AS text) AS cursor_id,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT c.* FROM categories c
CROSS JOIN params
WHERE EXISTS (
    SELECT 1
    FROM video_categories vc
    INNER JOIN videos v ON v.id = vc.video_id
    WHERE vc.category_id = c.id
      AND v.deleted_at IS NULL
)
  AND (
    params.cursor_name IS NULL
    OR unicode_lower(c.name) > unicode_lower(params.cursor_name)
    OR (unicode_lower(c.name) = unicode_lower(params.cursor_name) AND c.id > params.cursor_id)
  )
ORDER BY unicode_lower(c.name) ASC, c.id ASC
LIMIT (SELECT row_limit FROM params);

-- name: ListCategoriesWithVideosPageLatestDesc :many
WITH params AS (
    SELECT CAST(sqlc.narg('cursor_latest_video_at') AS text) AS cursor_latest_video_at,
           CAST(sqlc.narg('cursor_name') AS text) AS cursor_name,
           CAST(@cursor_id AS text) AS cursor_id,
           CAST(@row_limit AS integer) AS row_limit
),
category_stats AS (
    SELECT
        vc.category_id,
        MAX(v.start_download_at) AS latest_video_at,
        COUNT(DISTINCT vc.video_id) AS video_count
    FROM video_categories vc
    INNER JOIN videos v ON v.id = vc.video_id
    WHERE v.deleted_at IS NULL
    GROUP BY vc.category_id
)
SELECT c.*, category_stats.latest_video_at, category_stats.video_count
FROM categories c
INNER JOIN category_stats ON category_stats.category_id = c.id
CROSS JOIN params
WHERE (
    params.cursor_latest_video_at IS NULL
    OR category_stats.latest_video_at < params.cursor_latest_video_at
    OR (
        category_stats.latest_video_at = params.cursor_latest_video_at
        AND unicode_lower(c.name) > unicode_lower(params.cursor_name)
    )
    OR (
        category_stats.latest_video_at = params.cursor_latest_video_at
        AND unicode_lower(c.name) = unicode_lower(params.cursor_name)
        AND c.id > params.cursor_id
    )
)
ORDER BY category_stats.latest_video_at DESC, unicode_lower(c.name) ASC, c.id ASC
LIMIT (SELECT row_limit FROM params);

-- name: ListCategoriesWithVideosPageVideoCountDesc :many
WITH params AS (
    SELECT CAST(@cursor_video_count AS integer) AS cursor_video_count,
           CAST(sqlc.narg('cursor_name') AS text) AS cursor_name,
           CAST(@cursor_id AS text) AS cursor_id,
           CAST(@row_limit AS integer) AS row_limit
),
category_stats AS (
    SELECT
        vc.category_id,
        MAX(v.start_download_at) AS latest_video_at,
        COUNT(DISTINCT vc.video_id) AS video_count
    FROM video_categories vc
    INNER JOIN videos v ON v.id = vc.video_id
    WHERE v.deleted_at IS NULL
    GROUP BY vc.category_id
)
SELECT c.*, category_stats.latest_video_at, category_stats.video_count
FROM categories c
INNER JOIN category_stats ON category_stats.category_id = c.id
CROSS JOIN params
WHERE (
    params.cursor_name IS NULL
    OR category_stats.video_count < params.cursor_video_count
    OR (
        category_stats.video_count = params.cursor_video_count
        AND unicode_lower(c.name) > unicode_lower(params.cursor_name)
    )
    OR (
        category_stats.video_count = params.cursor_video_count
        AND unicode_lower(c.name) = unicode_lower(params.cursor_name)
        AND c.id > params.cursor_id
    )
)
ORDER BY category_stats.video_count DESC, unicode_lower(c.name) ASC, c.id ASC
LIMIT (SELECT row_limit FROM params);

-- name: ListCategoriesByIDs :many
SELECT * FROM categories WHERE id IN (sqlc.slice('ids'));

-- name: SearchCategories :many
-- Case-insensitive substring match on name. unicode_lower is registered by the
-- SQLite adapter so SQLite matches Go/Postgres Unicode case folding for category
-- search. Bind params once in a CTE with explicit casts so sqlc's SQLite output
-- stays typed through the repeated CASE/LIKE expressions.
WITH params AS (
    SELECT CAST(@query AS text) AS search_query,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT c.* FROM categories c
CROSS JOIN params
WHERE params.search_query = ''
   OR unicode_lower(c.name) LIKE '%' || unicode_lower(params.search_query) || '%'
ORDER BY
    CASE
        WHEN params.search_query = '' THEN 3
        WHEN unicode_lower(c.name) = unicode_lower(params.search_query) THEN 0
        WHEN unicode_lower(c.name) LIKE unicode_lower(params.search_query) || '%' THEN 1
        ELSE 2
    END,
    c.name
LIMIT (SELECT row_limit FROM params);

-- name: SearchCategoriesWithVideos :many
-- Same ranking contract as SearchCategories, restricted to categories linked to
-- at least one visible recording. unicode_lower is registered by the SQLite
-- adapter so SQLite matches Go/Postgres Unicode case folding for category search.
-- Bind params once in a CTE with explicit casts: this keeps sqlc's SQLite output
-- typed while avoiding missed @param rewrites in repeated CASE/LIKE expressions.
WITH params AS (
    SELECT CAST(@query AS text) AS search_query,
           CAST(@row_limit AS integer) AS row_limit
)
SELECT c.* FROM categories c
CROSS JOIN params
WHERE (params.search_query = ''
       OR unicode_lower(c.name) LIKE '%' || unicode_lower(params.search_query) || '%')
  AND EXISTS (
      SELECT 1
      FROM video_categories vc
      INNER JOIN videos v ON v.id = vc.video_id
      WHERE vc.category_id = c.id
        AND v.deleted_at IS NULL
  )
ORDER BY
    CASE
        WHEN params.search_query = '' THEN 3
        WHEN unicode_lower(c.name) = unicode_lower(params.search_query) THEN 0
        WHEN unicode_lower(c.name) LIKE unicode_lower(params.search_query) || '%' THEN 1
        ELSE 2
    END,
    c.name
LIMIT (SELECT row_limit FROM params);

-- name: ListCategoriesMissingGameMetadata :many
-- Helix /games is the source for both box_art_url and igdb_id. Keep this
-- broader than the historical box-art-only query so categories that already
-- have art but still lack igdb_id can become eligible for IGDB enrichment.
SELECT * FROM categories
WHERE (box_art_url IS NULL OR box_art_url = ''
       OR igdb_id IS NULL OR igdb_id = '')
  AND (game_metadata_checked_at IS NULL OR game_metadata_checked_at < ?)
ORDER BY name;

-- name: MarkCategoryGameMetadataChecked :exec
UPDATE categories
SET game_metadata_checked_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateCategoryGameMetadata :exec
-- Refresh the Twitch-side category metadata returned by Helix /games.
-- Empty inputs preserve the existing value so callers can safely write
-- whichever subset Twitch returned.
UPDATE categories
SET box_art_url = ifnull(nullif(?2, ''), box_art_url),
    igdb_id = ifnull(nullif(?3, ''), igdb_id),
    description = CASE
        WHEN nullif(?3, '') IS NOT NULL AND ifnull(igdb_id, '') <> nullif(?3, '')
        THEN NULL
        ELSE description
    END,
    description_checked_at = CASE
        WHEN nullif(?3, '') IS NOT NULL AND ifnull(igdb_id, '') <> nullif(?3, '')
        THEN NULL
        ELSE description_checked_at
    END,
    game_metadata_checked_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?1;

-- name: ListCategoriesMissingDescription :many
-- IGDB descriptions need a numeric igdb_id. Rows without one are left to the
-- Helix metadata sync first.
SELECT * FROM categories
WHERE igdb_id IS NOT NULL AND igdb_id <> ''
  AND (description IS NULL OR description = '')
  AND (description_checked_at IS NULL OR description_checked_at < ?)
ORDER BY name;

-- name: UpdateCategoryDescription :exec
UPDATE categories
SET description = ?2,
    description_checked_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?1;

-- name: MarkCategoryDescriptionChecked :exec
UPDATE categories
SET description_checked_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?;

-- name: GetCategorySearchCache :one
SELECT * FROM category_search_cache WHERE normalized_query = ?;

-- name: UpsertCategorySearchCache :one
INSERT INTO category_search_cache (normalized_query, category_ids, expires_at, last_accessed_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (normalized_query) DO UPDATE SET
    category_ids = excluded.category_ids,
    expires_at = excluded.expires_at,
    last_accessed_at = excluded.last_accessed_at,
    updated_at = datetime('now')
RETURNING *;

-- name: TouchCategorySearchCache :exec
UPDATE category_search_cache
SET last_accessed_at = ?,
    updated_at = datetime('now')
WHERE normalized_query = ?;

-- name: DeleteExpiredCategorySearchCache :exec
DELETE FROM category_search_cache WHERE expires_at < ?;

-- name: PruneCategorySearchCache :exec
DELETE FROM category_search_cache
WHERE normalized_query IN (
    SELECT normalized_query
    FROM category_search_cache
    ORDER BY last_accessed_at DESC, updated_at DESC, normalized_query ASC
    LIMIT -1 OFFSET ?
);
