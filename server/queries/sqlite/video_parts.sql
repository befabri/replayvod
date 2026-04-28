-- name: CreateVideoPart :one
INSERT INTO video_parts (
    video_id, part_index, filename, quality, fps, codec,
    segment_format, start_media_seq
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: FinalizeVideoPart :exec
UPDATE video_parts SET
    duration_seconds = ?,
    size_bytes = ?,
    thumbnail = ?,
    end_media_seq = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: GetVideoPart :one
SELECT * FROM video_parts WHERE id = ?;

-- name: GetVideoPartByIndex :one
SELECT * FROM video_parts WHERE video_id = ? AND part_index = ?;

-- name: ListVideoParts :many
SELECT * FROM video_parts WHERE video_id = ? ORDER BY part_index ASC;

-- name: CountVideoParts :one
SELECT COUNT(*) FROM video_parts WHERE video_id = ?;

-- name: DeleteVideoParts :exec
DELETE FROM video_parts WHERE video_id = ?;

-- name: HasFinalizedVideoParts :one
-- True when at least one part for this video has been remuxed and
-- persisted (size_bytes > 0). Mirrors the postgres query — see
-- queries/postgres/video_parts.sql for the rationale.
--
-- Single-line SELECT EXISTS form: sqlc-sqlite's engine miscompiles
-- multi-line EXISTS bodies, corrupting the const literal AND the
-- next query in the file (we hit both: a truncated `has_finalized`
-- alias and a clobbered DeleteVideoParts). One-line form sidesteps
-- the parser bug.
SELECT EXISTS (SELECT 1 FROM video_parts WHERE video_id = ? AND size_bytes > 0) AS has_finalized;
