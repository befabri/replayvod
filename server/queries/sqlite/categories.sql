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
