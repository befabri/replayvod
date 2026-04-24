-- name: LinkStreamCategory :exec
INSERT INTO stream_categories (stream_id, category_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: LinkVideoCategory :exec
INSERT INTO video_categories (video_id, category_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: LinkStreamTag :exec
INSERT INTO stream_tags (stream_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: LinkVideoTag :exec
INSERT INTO video_tags (video_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: ListTagsForVideo :many
SELECT t.* FROM tags t
INNER JOIN video_tags vt ON vt.tag_id = t.id
WHERE vt.video_id = ?
ORDER BY t.name;

-- name: AddVideoRequest :exec
INSERT INTO video_requests (video_id, user_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: ListVideoRequestsForUser :many
SELECT v.* FROM videos v
INNER JOIN video_requests vr ON vr.video_id = v.id
WHERE vr.user_id = ? AND v.deleted_at IS NULL
ORDER BY vr.requested_at DESC
LIMIT ? OFFSET ?;

-- name: CloseOtherOpenVideoCategorySpans :exec
-- Paired with InsertVideoCategorySpan to emulate pg's CTE-driven
-- upsert. Called first inside the same tx as InsertVideoCategorySpan;
-- closes only the spans whose category_id differs from the new one.
UPDATE video_category_spans
   SET ended_at = @at_time,
       duration_seconds = duration_seconds + ((julianday(@at_time) - julianday(started_at)) * 86400.0)
 WHERE video_id = @video_id
   AND ended_at IS NULL
   AND category_id <> @category_id;

-- name: InsertVideoCategorySpan :exec
-- @at_time: see CloseOtherOpenVideoTitleSpans for why
-- the string cast is load-bearing.
INSERT INTO video_category_spans (video_id, category_id, started_at)
VALUES (@video_id, @category_id, @at_time)
ON CONFLICT (video_id, category_id) WHERE ended_at IS NULL DO NOTHING;

-- name: CloseOpenVideoCategorySpans :exec
UPDATE video_category_spans
   SET ended_at = @at_time,
       duration_seconds = duration_seconds + ((julianday(@at_time) - julianday(started_at)) * 86400.0)
 WHERE video_id = @video_id
   AND ended_at IS NULL;

-- name: ResumeVideoCategorySpan :exec
-- See queries/sqlite/titles.sql ResumeVideoTitleSpan for why this
-- uses positional ?1/?2 instead of @video_id/@at_time.
INSERT INTO video_category_spans (video_id, category_id, started_at)
SELECT ?1, latest.category_id, ?2
FROM (
    SELECT category_id
    FROM video_category_spans
    WHERE video_id = ?1
    ORDER BY started_at DESC, id DESC
    LIMIT 1
) latest
WHERE NOT EXISTS (
    SELECT 1 FROM video_category_spans
    WHERE video_id = ?1 AND ended_at IS NULL
);

-- name: ListCategorySpansForVideo :many
SELECT
    c.id,
    c.name,
    c.box_art_url,
    c.igdb_id,
    c.created_at,
    c.updated_at,
    vcs.started_at,
    vcs.ended_at,
    (vcs.duration_seconds + CASE
        WHEN vcs.ended_at IS NULL THEN ((julianday('now') - julianday(vcs.started_at)) * 86400.0)
        ELSE 0
    END) AS duration_seconds
FROM categories c
INNER JOIN video_category_spans vcs ON vcs.category_id = c.id
WHERE vcs.video_id = ?
ORDER BY vcs.started_at ASC, vcs.id ASC;

-- name: ListPrimaryCategoriesForVideos :many
-- For each requested video, group spans by category and return the
-- aggregate rows ordered so the first row per video_id is the
-- "primary" category (most total duration, earliest first-seen,
-- then name). The adapter takes the first row per video_id since
-- SQLite lacks DISTINCT ON.
SELECT vcs.video_id,
       c.id,
       c.name,
       c.box_art_url,
       c.igdb_id,
       c.created_at,
       c.updated_at,
       SUM(vcs.duration_seconds + CASE
           WHEN vcs.ended_at IS NULL THEN ((julianday('now') - julianday(vcs.started_at)) * 86400.0)
           ELSE 0
       END) AS duration_seconds,
       MIN(vcs.started_at) AS first_seen_at
FROM video_category_spans vcs
INNER JOIN categories c ON c.id = vcs.category_id
WHERE vcs.video_id IN (sqlc.slice('video_ids'))
GROUP BY vcs.video_id, vcs.category_id, c.id, c.name, c.box_art_url, c.igdb_id, c.created_at, c.updated_at
ORDER BY
    vcs.video_id ASC,
    duration_seconds DESC,
    first_seen_at ASC,
    c.name ASC;
