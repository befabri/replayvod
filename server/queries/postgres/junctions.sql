-- name: LinkStreamCategory :exec
INSERT INTO stream_categories (stream_id, category_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: LinkVideoCategory :exec
INSERT INTO video_categories (video_id, category_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: LinkStreamTag :exec
INSERT INTO stream_tags (stream_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: LinkVideoTag :exec
INSERT INTO video_tags (video_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListTagsForVideo :many
SELECT t.* FROM tags t
INNER JOIN video_tags vt ON vt.tag_id = t.id
WHERE vt.video_id = $1
ORDER BY t.name;

-- name: AddVideoRequest :exec
INSERT INTO video_requests (video_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListVideoRequestsForUser :many
SELECT v.* FROM videos v
INNER JOIN video_requests vr ON vr.video_id = v.id
WHERE vr.user_id = $1 AND v.deleted_at IS NULL
ORDER BY vr.requested_at DESC
LIMIT $2 OFFSET $3;

-- name: UpsertVideoCategorySpan :exec
-- Category analogue of UpsertVideoTitleSpan; see that comment for the
-- close-then-insert rationale.
WITH close_previous AS (
    UPDATE video_category_spans vcs
       SET ended_at = sqlc.arg('at_time')::timestamptz,
           duration_seconds = vcs.duration_seconds + EXTRACT(EPOCH FROM (sqlc.arg('at_time')::timestamptz - vcs.started_at))
     WHERE vcs.video_id = sqlc.arg('video_id')
       AND vcs.ended_at IS NULL
       AND vcs.category_id <> sqlc.arg('category_id')
)
INSERT INTO video_category_spans (video_id, category_id, started_at)
VALUES (sqlc.arg('video_id'), sqlc.arg('category_id'), sqlc.arg('at_time')::timestamptz)
ON CONFLICT (video_id, category_id) WHERE ended_at IS NULL DO NOTHING;

-- name: CloseOpenVideoCategorySpans :exec
UPDATE video_category_spans vcs
   SET ended_at = sqlc.arg('at_time')::timestamptz,
       duration_seconds = vcs.duration_seconds + EXTRACT(EPOCH FROM (sqlc.arg('at_time')::timestamptz - vcs.started_at))
 WHERE vcs.video_id = sqlc.arg('video_id')
   AND vcs.ended_at IS NULL;

-- name: ResumeVideoCategorySpan :exec
WITH latest AS (
    SELECT category_id
    FROM video_category_spans
    WHERE video_id = sqlc.arg('video_id')
    ORDER BY started_at DESC, id DESC
    LIMIT 1
)
INSERT INTO video_category_spans (video_id, category_id, started_at)
SELECT sqlc.arg('video_id'), latest.category_id, sqlc.arg('at_time')::timestamptz
FROM latest
WHERE NOT EXISTS (
    SELECT 1 FROM video_category_spans
    WHERE video_id = sqlc.arg('video_id') AND ended_at IS NULL
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
        WHEN vcs.ended_at IS NULL THEN EXTRACT(EPOCH FROM (NOW() - vcs.started_at))
        ELSE 0
    END)::double precision AS duration_seconds
FROM categories c
INNER JOIN video_category_spans vcs ON vcs.category_id = c.id
WHERE vcs.video_id = $1
ORDER BY vcs.started_at ASC, vcs.id ASC;

-- name: ListPrimaryCategoriesForVideos :many
-- Pick the single most-played category per video, ordered within the
-- video by total span duration then first-seen time then name. Used
-- by the video list response to render a stable "primary category"
-- label without round-tripping every row.
SELECT DISTINCT ON (agg.video_id)
    agg.video_id,
    c.id,
    c.name,
    c.box_art_url,
    c.igdb_id,
    c.created_at,
    c.updated_at,
    agg.duration_seconds
FROM (
    SELECT
        vcs.video_id,
        vcs.category_id,
        SUM(vcs.duration_seconds + CASE
            WHEN vcs.ended_at IS NULL THEN EXTRACT(EPOCH FROM (NOW() - vcs.started_at))
            ELSE 0
        END)::double precision AS duration_seconds,
        MIN(vcs.started_at) AS first_seen_at
    FROM video_category_spans vcs
    WHERE vcs.video_id = ANY(@video_ids::bigint[])
    GROUP BY vcs.video_id, vcs.category_id
) agg
INNER JOIN categories c ON c.id = agg.category_id
ORDER BY
    agg.video_id,
    duration_seconds DESC,
    agg.first_seen_at ASC,
    c.name ASC;
