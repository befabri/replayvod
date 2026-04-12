-- name: LinkStreamCategory :exec
INSERT INTO stream_categories (stream_id, category_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: LinkVideoCategory :exec
INSERT INTO video_categories (video_id, category_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: LinkStreamTag :exec
INSERT INTO stream_tags (stream_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: LinkVideoTag :exec
INSERT INTO video_tags (video_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: ListCategoriesForVideo :many
SELECT c.* FROM categories c
INNER JOIN video_categories vc ON vc.category_id = c.id
WHERE vc.video_id = ?
ORDER BY c.name;

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
