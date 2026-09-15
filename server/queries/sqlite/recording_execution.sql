-- name: GetVideoForUpdate :one
-- The first transaction statement reserves SQLite's writer before reading.
UPDATE videos SET id = id WHERE id = ? RETURNING *;

-- name: SetJobExecution :execrows
UPDATE jobs SET execution_id = ?2, accepts_metadata = ?3,
    status = 'RUNNING', started_at = COALESCE(started_at, datetime('now')), updated_at = datetime('now')
WHERE id = ?1 AND status IN ('PENDING', 'RUNNING');

-- name: StopJobMetadata :execrows
UPDATE jobs SET accepts_metadata = 0 WHERE id = ?1 AND execution_id = ?2 AND status = 'RUNNING';

-- name: RequestJobStop :exec
UPDATE jobs SET stop_requested = 1, accepts_metadata = 0, updated_at = datetime('now')
WHERE id = ? AND status IN ('PENDING', 'RUNNING');

-- name: CheckpointAttempt :execrows
UPDATE jobs SET resume_state = ?3, updated_at = datetime('now')
WHERE jobs.id = ?1 AND jobs.execution_id = ?2 AND jobs.status = 'RUNNING'
  AND EXISTS (SELECT 1 FROM videos WHERE videos.id = jobs.video_id
      AND videos.job_id = jobs.id AND videos.status = 'RUNNING' AND videos.deleted_at IS NULL);

-- name: ListRecoveryJobs :many
SELECT jobs.* FROM jobs JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.id > ?1 AND (jobs.status = 'RUNNING' OR (jobs.status = 'PENDING' AND videos.source = 'live'))
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL
ORDER BY jobs.id LIMIT ?2;

-- name: ListQueuedArchiveJobs :many
SELECT jobs.id AS job_id, videos.id AS video_id, videos.start_download_at AS queued_at FROM jobs JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.status = 'PENDING' AND videos.status = 'PENDING' AND videos.source = 'vod' AND videos.deleted_at IS NULL
AND (videos.start_download_at > sqlc.arg(after_start) OR (videos.start_download_at = sqlc.arg(after_start) AND videos.id > CAST(sqlc.arg(after_id) AS BIGINT)))
ORDER BY videos.start_download_at, videos.id LIMIT sqlc.arg(batch_limit);

-- name: ListStoppedJobs :many
SELECT jobs.* FROM jobs JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.id > ?1 AND jobs.stop_requested = 1 AND jobs.status IN ('PENDING', 'RUNNING')
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL
ORDER BY jobs.id LIMIT ?2;
