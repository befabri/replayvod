-- name: GetMediaPublication :one
SELECT * FROM media_publications WHERE key = ?1;

-- name: BeginMediaPublication :one
INSERT INTO media_publications (key, video_id, digest, size_bytes, unresolved)
VALUES (?1, ?2, ?3, ?4, 1)
ON CONFLICT (key) DO UPDATE SET unresolved = 1
WHERE media_publications.video_id = EXCLUDED.video_id AND media_publications.digest = EXCLUDED.digest
  AND media_publications.delete_requested = 0
RETURNING *;

-- name: ConfirmMediaPublication :exec
UPDATE media_publications SET unresolved = 0 WHERE key = ?1 AND digest = ?2;

-- name: RequestMediaPublicationDelete :exec
UPDATE media_publications SET delete_requested = 1 WHERE key = ?1;

-- name: DeleteMediaPublication :exec
DELETE FROM media_publications WHERE key = ?1 AND unresolved = 0;

-- name: ListMediaPublications :many
SELECT * FROM media_publications WHERE key > ?1 ORDER BY key LIMIT ?2;

-- name: ListRecordingPublications :many
SELECT * FROM media_publications WHERE video_id = ?1 AND key > ?2 ORDER BY key LIMIT ?3;
