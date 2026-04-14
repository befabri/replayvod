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

-- name: ListTitlesForVideo :many
-- Ordered by linked_at, not titles.id — the dedup-row id reflects
-- creation order across the whole titles table, not the order a
-- specific video saw each title. Without this the history UI
-- misorders titles whenever a stream reuses a name from a prior
-- broadcast.
SELECT t.* FROM titles t
INNER JOIN video_titles vt ON vt.title_id = t.id
WHERE vt.video_id = $1
ORDER BY vt.linked_at;
