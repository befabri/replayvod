-- name: UpsertTitle :one
INSERT INTO titles (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: LinkStreamTitle :exec
INSERT INTO stream_titles (stream_id, title_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: LinkVideoTitle :exec
INSERT INTO video_titles (video_id, title_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListTitlesForStream :many
SELECT t.* FROM titles t
INNER JOIN stream_titles st ON st.title_id = t.id
WHERE st.stream_id = $1
ORDER BY t.id;

-- name: UpsertVideoTitleSpan :exec
-- Close the currently-open span if its title differs from the new
-- one, then insert the new span. The CTE branch never runs when the
-- incoming title matches the open row, so the ON CONFLICT leaves
-- the existing span untouched.
--
-- Uses sqlc.arg() + ::timestamptz cast (not the @-shorthand form)
-- because sqlc's @-rewriter mangles the SQL inside
-- EXTRACT(EPOCH FROM (@param - column)) expressions. The cast also
-- forces non-nullable Go types for the generated param struct.
WITH close_previous AS (
    UPDATE video_title_spans vts
       SET ended_at = sqlc.arg('at_time')::timestamptz,
           duration_seconds = vts.duration_seconds + EXTRACT(EPOCH FROM (sqlc.arg('at_time')::timestamptz - vts.started_at))
     WHERE vts.video_id = sqlc.arg('video_id')
       AND vts.ended_at IS NULL
       AND vts.title_id <> sqlc.arg('title_id')
)
INSERT INTO video_title_spans (video_id, title_id, started_at)
VALUES (sqlc.arg('video_id'), sqlc.arg('title_id'), sqlc.arg('at_time')::timestamptz)
ON CONFLICT (video_id, title_id) WHERE ended_at IS NULL DO NOTHING;

-- name: CloseOpenVideoTitleSpans :exec
-- Stamp ended_at + add the elapsed interval to duration_seconds for
-- every still-open title span of this video. Used when the recording
-- terminates (clean end or cancelled) so the history shows a finite
-- duration instead of an open-ended span.
UPDATE video_title_spans vts
   SET ended_at = sqlc.arg('at_time')::timestamptz,
       duration_seconds = vts.duration_seconds + EXTRACT(EPOCH FROM (sqlc.arg('at_time')::timestamptz - vts.started_at))
 WHERE vts.video_id = sqlc.arg('video_id')
   AND vts.ended_at IS NULL;

-- name: ResumeVideoTitleSpan :exec
-- After CloseOpenVideoTitleSpans ran against a prior failed/
-- suspended recording, reopen a new span starting at at_time
-- carrying the most recent title — unless one is already open.
-- Idempotent across retry loops.
WITH latest AS (
    SELECT title_id
    FROM video_title_spans
    WHERE video_id = sqlc.arg('video_id')
    ORDER BY started_at DESC, id DESC
    LIMIT 1
)
INSERT INTO video_title_spans (video_id, title_id, started_at)
SELECT sqlc.arg('video_id'), latest.title_id, sqlc.arg('at_time')::timestamptz
FROM latest
WHERE NOT EXISTS (
    SELECT 1 FROM video_title_spans
    WHERE video_id = sqlc.arg('video_id') AND ended_at IS NULL
);

-- name: ListTitleSpansForVideo :many
-- One row per title span ordered by when the stream first set that
-- title. Still-open spans expose (NOW() - started_at) as their
-- live contribution to duration_seconds so the UI reads a live
-- duration until the recording closes.
SELECT
    t.id,
    t.name,
    t.created_at,
    vts.started_at,
    vts.ended_at,
    (vts.duration_seconds + CASE
        WHEN vts.ended_at IS NULL THEN EXTRACT(EPOCH FROM (NOW() - vts.started_at))
        ELSE 0
    END)::double precision AS duration_seconds
FROM titles t
INNER JOIN video_title_spans vts ON vts.title_id = t.id
WHERE vts.video_id = $1
ORDER BY vts.started_at ASC, vts.id ASC;
