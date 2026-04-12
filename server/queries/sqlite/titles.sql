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

-- name: ListTitlesForVideo :many
SELECT t.* FROM titles t
INNER JOIN video_titles vt ON vt.title_id = t.id
WHERE vt.video_id = ?
ORDER BY t.id;
