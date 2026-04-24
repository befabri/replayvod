-- name: UpsertTitle :one
INSERT INTO titles (name) VALUES (?)
ON CONFLICT (name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: LinkStreamTitle :exec
INSERT INTO stream_titles (stream_id, title_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: LinkVideoTitle :exec
INSERT INTO video_titles (video_id, title_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: ListTitlesForStream :many
SELECT t.* FROM titles t
INNER JOIN stream_titles st ON st.title_id = t.id
WHERE st.stream_id = ?
ORDER BY t.id;

-- name: CloseOtherOpenVideoTitleSpans :exec
-- Paired with InsertVideoTitleSpan to emulate pg's CTE-driven upsert.
-- Called first inside the same tx as InsertVideoTitleSpan; closes only
-- the spans whose title_id differs from the new one.
--
-- @at_time forces sqlc to type @at_time as `string`,
-- not `sql.NullTime`. The adapter pre-formats using formatTime() to
-- the "2006-01-02 15:04:05" shape SQLite's julianday() accepts;
-- modernc.org/sqlite's native time.Time binding produces RFC3339
-- with the `T` separator and `Z` suffix, which julianday() treats
-- as NULL, silently corrupting the duration sum.
UPDATE video_title_spans
   SET ended_at = @at_time,
       duration_seconds = duration_seconds + ((julianday(@at_time) - julianday(started_at)) * 86400.0)
 WHERE video_id = @video_id
   AND ended_at IS NULL
   AND title_id <> @title_id;

-- name: InsertVideoTitleSpan :exec
-- The INSERT half of the upsert. The partial unique index on
-- (video_id, title_id) WHERE ended_at IS NULL keeps the same-title
-- re-enter case a no-op.
INSERT INTO video_title_spans (video_id, title_id, started_at)
VALUES (@video_id, @title_id, @at_time)
ON CONFLICT (video_id, title_id) WHERE ended_at IS NULL DO NOTHING;

-- name: CloseOpenVideoTitleSpans :exec
UPDATE video_title_spans
   SET ended_at = @at_time,
       duration_seconds = duration_seconds + ((julianday(@at_time) - julianday(started_at)) * 86400.0)
 WHERE video_id = @video_id
   AND ended_at IS NULL;

-- name: ResumeVideoTitleSpan :exec
-- sqlc-sqlite's @-rewriter misses some @video_id occurrences when
-- the param is referenced from three clauses in the same statement,
-- leaving literal "@video_id" tokens for the driver to choke on.
-- Use positional ?1 / ?2 here to force sqlc to bind uniformly.
INSERT INTO video_title_spans (video_id, title_id, started_at)
SELECT ?1, latest.title_id, ?2
FROM (
    SELECT title_id
    FROM video_title_spans
    WHERE video_id = ?1
    ORDER BY started_at DESC, id DESC
    LIMIT 1
) latest
WHERE NOT EXISTS (
    SELECT 1 FROM video_title_spans
    WHERE video_id = ?1 AND ended_at IS NULL
);

-- name: ListTitleSpansForVideo :many
-- julianday('now') is UTC per SQLite docs; matches pg's NOW() at UTC,
-- which the adapter forces in its connection setup. The
-- duration_seconds expression ends up typed `interface{}` in the
-- generated code because sqlc can't infer a REAL through the CASE
-- branches (a CAST(... AS REAL) wrapper here crashes sqlc-sqlite's
-- parser when another query in this file uses sqlc.slice). The
-- adapter asserts the scan value to float64.
SELECT
    t.id,
    t.name,
    t.created_at,
    vts.started_at,
    vts.ended_at,
    (vts.duration_seconds + CASE
        WHEN vts.ended_at IS NULL THEN ((julianday('now') - julianday(vts.started_at)) * 86400.0)
        ELSE 0
    END) AS duration_seconds
FROM titles t
INNER JOIN video_title_spans vts ON vts.title_id = t.id
WHERE vts.video_id = ?
ORDER BY vts.started_at ASC, vts.id ASC;
