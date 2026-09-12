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

-- name: GetNextQueuedArchiveJob :one
-- Only the job a queued video currently points at qualifies, so a job left
-- behind by an earlier attempt can never be started.
SELECT jobs.* FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.status = 'PENDING' AND videos.status = 'PENDING'
  AND videos.source = 'vod' AND videos.deleted_at IS NULL
ORDER BY videos.start_download_at ASC, videos.id ASC LIMIT 1;

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
-- Recover interrupted attempts, including live jobs saved before their worker
-- claimed them. Pending archives remain controlled by the archive queue.
SELECT jobs.* FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE (jobs.status = 'RUNNING' OR (jobs.status = 'PENDING' AND videos.source = 'live'))
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL
ORDER BY jobs.started_at ASC;

-- name: ListFailedJobsForRetry :many
-- Scheduler retry query: FAILED jobs whose finished_at is older than
-- the retry cooldown. Caller filters further (e.g. only retry if the
-- video's stream is still live).
SELECT * FROM jobs
WHERE status = 'FAILED' AND finished_at IS NOT NULL AND finished_at < $1
ORDER BY finished_at ASC LIMIT $2;

-- name: ListRunningLiveBroadcasters :many
SELECT DISTINCT jobs.broadcaster_id FROM jobs
JOIN videos ON videos.id = jobs.video_id AND videos.job_id = jobs.id
WHERE jobs.status = 'RUNNING' AND videos.source = 'live'
  AND videos.status IN ('PENDING', 'RUNNING') AND videos.deleted_at IS NULL;
