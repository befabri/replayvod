-- name: UpsertTask :one
-- Registered on scheduler startup. Only writes the descriptive columns;
-- existing rows keep their runtime state (last_run_at, last_status,
-- etc.) so a redeploy doesn't reset counters.
INSERT INTO tasks (name, description, interval_seconds)
VALUES ($1, $2, $3)
ON CONFLICT (name) DO UPDATE
SET description      = EXCLUDED.description,
    interval_seconds = EXCLUDED.interval_seconds,
    updated_at       = NOW()
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE name = $1;

-- name: ListTasks :many
SELECT * FROM tasks ORDER BY name;

-- name: ListDueTasks :many
-- Scheduler tick path: enabled tasks whose next_run_at has passed.
-- The partial index idx_tasks_next_run_at keeps this O(log n).
SELECT * FROM tasks
WHERE is_enabled = TRUE
  AND interval_seconds > 0
  AND (next_run_at IS NULL OR next_run_at <= NOW())
ORDER BY next_run_at NULLS FIRST;

-- name: MarkTaskRunning :exec
UPDATE tasks
SET last_status = 'running',
    last_run_at = NOW(),
    last_error  = NULL,
    updated_at  = NOW()
WHERE name = $1;

-- name: MarkTaskSuccess :exec
UPDATE tasks
SET last_status      = 'success',
    last_duration_ms = $2,
    next_run_at      = CASE
        WHEN interval_seconds > 0
        THEN NOW() + (interval_seconds * INTERVAL '1 second')
        ELSE NULL
    END,
    last_error       = NULL,
    updated_at       = NOW()
WHERE name = $1;

-- name: MarkTaskFailed :exec
UPDATE tasks
SET last_status      = 'failed',
    last_duration_ms = $2,
    last_error       = $3,
    next_run_at      = CASE
        WHEN interval_seconds > 0
        THEN NOW() + (interval_seconds * INTERVAL '1 second')
        ELSE NULL
    END,
    updated_at       = NOW()
WHERE name = $1;

-- name: SetTaskEnabled :one
UPDATE tasks
SET is_enabled  = $2,
    next_run_at = CASE
        WHEN $2 = TRUE AND interval_seconds > 0 AND next_run_at IS NULL
        THEN NOW()
        ELSE next_run_at
    END,
    updated_at  = NOW()
WHERE name = $1
RETURNING *;

-- name: SetTaskNextRun :exec
-- Manual "run now" path — set next_run_at to now so the scheduler picks
-- it up on the next tick. Separate from SetTaskEnabled so the caller
-- can request a one-shot run without changing the enabled flag.
UPDATE tasks
SET next_run_at = NOW(),
    updated_at  = NOW()
WHERE name = $1;
