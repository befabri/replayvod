-- name: CreateVideoPart :one
-- end_media_seq is deliberately omitted: its real value is only known
-- at FinalizeVideoPart time. Creating a part with a placeholder
-- produced rows indistinguishable from zero-length recordings when a
-- job failed before finalize.
INSERT INTO video_parts (
    video_id, part_index, filename, quality, codec, segment_format,
    start_media_seq
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: FinalizeVideoPart :exec
UPDATE video_parts SET
    duration_seconds = $2,
    size_bytes = $3,
    thumbnail = $4,
    end_media_seq = $5,
    updated_at = NOW()
WHERE id = $1;

-- name: GetVideoPart :one
SELECT * FROM video_parts WHERE id = $1;

-- name: GetVideoPartByIndex :one
-- The "current part" lookup used by resume logic — given a video and
-- a part_index, return the row without pulling the whole list.
SELECT * FROM video_parts WHERE video_id = $1 AND part_index = $2;

-- name: ListVideoParts :many
SELECT * FROM video_parts WHERE video_id = $1 ORDER BY part_index ASC;

-- name: CountVideoParts :one
SELECT COUNT(*) FROM video_parts WHERE video_id = $1;

-- name: DeleteVideoParts :exec
DELETE FROM video_parts WHERE video_id = $1;
