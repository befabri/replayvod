-- name: CreateVideoPart :one
INSERT INTO video_parts (
    video_id, part_index, filename, quality, codec, segment_format,
    start_media_seq, end_media_seq
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: FinalizeVideoPart :exec
UPDATE video_parts SET
    duration_seconds = ?,
    size_bytes = ?,
    thumbnail = ?,
    end_media_seq = ?
WHERE id = ?;

-- name: GetVideoPart :one
SELECT * FROM video_parts WHERE id = ?;

-- name: ListVideoParts :many
SELECT * FROM video_parts WHERE video_id = ? ORDER BY part_index ASC;

-- name: CountVideoParts :one
SELECT COUNT(*) FROM video_parts WHERE video_id = ?;

-- name: DeleteVideoParts :exec
DELETE FROM video_parts WHERE video_id = ?;
