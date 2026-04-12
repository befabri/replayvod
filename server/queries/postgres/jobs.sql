-- name: CreateJob :one
INSERT INTO jobs (id, video_id, broadcaster_id, status, resume_state)
VALUES ($1, $2, $3, 'PENDING', $4)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = $1;

-- name: GetJobByVideoID :one
-- The most recent job for a video. Used to wire resume state back to
-- the download service on restart: a video can accumulate multiple
-- FAILED jobs + one DONE, and we want the live/terminal one.
SELECT * FROM jobs WHERE video_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: GetActiveJobByBroadcaster :one
-- Broadcaster-level idempotency check. Returns PENDING or RUNNING
-- only — terminal rows don't block a new job.
SELECT * FROM jobs
WHERE broadcaster_id = $1 AND status IN ('PENDING', 'RUNNING')
ORDER BY created_at DESC LIMIT 1;

-- name: MarkJobRunning :exec
UPDATE jobs SET status = 'RUNNING', started_at = NOW(), updated_at = NOW()
WHERE id = $1;

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

-- name: UpdateJobResumeState :exec
-- Hot path: called after every segment completion, stage transition,
-- and accepted gap. Single UPDATE keeps the write atomic with respect
-- to the frontier-advance logic in the downloader.
UPDATE jobs SET resume_state = $2, updated_at = NOW() WHERE id = $1;

-- name: ListRunningJobs :many
-- On server startup: every row here is a job whose process crashed
-- mid-execution. The downloader's resume path runs for each.
SELECT * FROM jobs WHERE status = 'RUNNING' ORDER BY started_at ASC;

-- name: ListFailedJobsForRetry :many
-- Scheduler retry query: FAILED jobs whose finished_at is older than
-- the retry cooldown. Caller filters further (e.g. only retry if the
-- video's stream is still live).
SELECT * FROM jobs
WHERE status = 'FAILED' AND finished_at IS NOT NULL AND finished_at < $1
ORDER BY finished_at ASC LIMIT $2;
