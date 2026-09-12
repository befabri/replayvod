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

-- name: GetNextQueuedArchiveJob :one
-- Only the job a queued video currently points at qualifies, so a job left
-- behind by an earlier attempt can never be started.
SELECT jobs.* FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.status = 'PENDING' AND videos.status = 'PENDING'
  AND videos.source = 'vod' AND videos.deleted_at IS NULL
ORDER BY videos.start_download_at ASC, videos.id ASC LIMIT 1;

-- name: MarkJobRunning :exec
UPDATE jobs SET status = 'RUNNING', started_at = datetime('now'), updated_at = datetime('now')
WHERE id = ?;

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
    error = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateJobResumeState :exec
UPDATE jobs SET resume_state = ?, updated_at = datetime('now') WHERE id = ?;

-- name: ListRunningJobs :many
SELECT jobs.* FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE (jobs.status = 'RUNNING' OR (jobs.status = 'PENDING' AND videos.source = 'live'))
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL
ORDER BY jobs.started_at ASC;

-- name: ListFailedJobsForRetry :many
SELECT * FROM jobs
WHERE status = 'FAILED' AND finished_at IS NOT NULL AND finished_at < ?
ORDER BY finished_at ASC LIMIT ?;

-- name: ListRunningLiveBroadcasters :many
SELECT DISTINCT jobs.broadcaster_id FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.status = 'RUNNING' AND videos.source = 'live'
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL;
