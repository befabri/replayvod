-- name: CreateJob :one
INSERT INTO jobs (id, video_id, broadcaster_id, status, resume_state, attempt)
VALUES (?, ?, ?, 'PENDING', ?, ?)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = ?;

-- name: GetJobByVideoID :one
SELECT * FROM jobs WHERE video_id = ? ORDER BY created_at DESC LIMIT 1;

-- name: GetActiveLiveJobByBroadcaster :one
-- Live only: a queued or running archive for the same channel must neither
-- block a live recording nor receive its channel.update metadata.
SELECT jobs.* FROM jobs
JOIN videos ON videos.id = jobs.video_id
WHERE videos.job_id = jobs.id AND jobs.broadcaster_id = ? AND jobs.status IN ('PENDING', 'RUNNING')
  AND videos.source = 'live'
ORDER BY jobs.created_at DESC LIMIT 1;

-- name: MarkJobDone :exec
UPDATE jobs SET
    status = 'DONE',
    finished_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?;

-- name: MarkJobFailed :exec
UPDATE jobs SET
    status = 'FAILED',
    finished_at = datetime('now'),
    error = @err_msg,
    updated_at = datetime('now')
WHERE id = @id;

-- name: ListRunningLiveBroadcasters :many
SELECT DISTINCT jobs.broadcaster_id FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.status = 'RUNNING' AND videos.source = 'live'
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL;
