-- name: CreateJob :one
INSERT INTO jobs (id, video_id, broadcaster_id, status, resume_state, attempt)
VALUES ($1, $2, $3, 'PENDING', $4, $5)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = $1;

-- name: GetJobByVideoID :one
-- The most recent job for a video. Used to wire resume state back to
-- the download service on restart: a video can accumulate multiple
-- FAILED jobs + one DONE, and we want the live/terminal one.
SELECT * FROM jobs WHERE video_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: GetActiveLiveJobByBroadcaster :one
-- Broadcaster-level idempotency check. Returns PENDING or RUNNING
-- only — terminal rows don't block a new job. Live only: a queued or
-- running archive for the same channel must neither block a live
-- recording nor receive its channel.update metadata.
SELECT jobs.* FROM jobs
JOIN videos ON videos.id = jobs.video_id
WHERE videos.job_id = jobs.id AND jobs.broadcaster_id = $1 AND jobs.status IN ('PENDING', 'RUNNING')
  AND videos.source = 'live'
ORDER BY jobs.created_at DESC LIMIT 1;

-- name: MarkJobDone :exec
UPDATE jobs SET
    status = 'DONE',
    finished_at = NOW(),
    updated_at = NOW()
WHERE id = $1;

-- name: MarkJobFailed :exec
UPDATE jobs SET
    status = 'FAILED',
    finished_at = NOW(),
    error = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: ListRunningLiveBroadcasters :many
SELECT DISTINCT jobs.broadcaster_id FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.status = 'RUNNING' AND videos.source = 'live'
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL;
