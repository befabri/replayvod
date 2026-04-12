-- name: CreateJob :one
INSERT INTO jobs (id, video_id, broadcaster_id, status, resume_state)
VALUES (?, ?, ?, 'PENDING', ?)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = ?;

-- name: GetJobByVideoID :one
SELECT * FROM jobs WHERE video_id = ? ORDER BY created_at DESC LIMIT 1;

-- name: GetActiveJobByBroadcaster :one
SELECT * FROM jobs
WHERE broadcaster_id = ? AND status IN ('PENDING', 'RUNNING')
ORDER BY created_at DESC LIMIT 1;

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
SELECT * FROM jobs WHERE status = 'RUNNING' ORDER BY started_at ASC;

-- name: ListFailedJobsForRetry :many
SELECT * FROM jobs
WHERE status = 'FAILED' AND finished_at IS NOT NULL AND finished_at < ?
ORDER BY finished_at ASC LIMIT ?;
