-- name: UpsertTask :one
-- Registration marks configured tasks available while preserving the operator's
-- pause and prior execution history.
INSERT INTO tasks (name, description, interval_seconds, is_available)
VALUES ($1, $2, $3, TRUE)
ON CONFLICT (name) DO UPDATE
SET is_available     = TRUE,
    description      = EXCLUDED.description,
    interval_seconds = EXCLUDED.interval_seconds,
    updated_at       = NOW()
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE name = $1;

-- name: ListTasks :many
SELECT * FROM tasks ORDER BY name;

-- name: ListDueTasks :many
-- Eligible work includes scheduled intervals and explicit one-shot requests.
SELECT * FROM tasks
WHERE is_available = TRUE AND is_enabled = TRUE
  AND last_status <> 'running'
  AND (interval_seconds > 0 OR next_run_at IS NOT NULL)
  AND (next_run_at IS NULL OR next_run_at <= NOW())
ORDER BY next_run_at NULLS FIRST;

-- name: SetTaskEnabled :one
UPDATE tasks
SET is_enabled  = $2,
    next_run_at = CASE
        WHEN $2 = TRUE AND is_available = TRUE AND interval_seconds > 0 AND next_run_at IS NULL
        THEN NOW()
        ELSE next_run_at
    END,
    updated_at  = NOW()
WHERE name = $1
RETURNING *;

-- name: SetTaskNextRun :one
-- Manual "run now" path — set next_run_at to now so the scheduler picks
-- it up on the next tick. Separate from SetTaskEnabled so the caller
-- can request a one-shot run without changing the enabled flag.
UPDATE tasks
SET next_run_at = NOW(),
    updated_at  = NOW()
WHERE name = $1 AND is_available = TRUE
RETURNING *;

-- name: ScheduleTaskIfEnabled :one
UPDATE tasks SET next_run_at = NOW(), updated_at = NOW()
WHERE name = $1 AND is_enabled = TRUE AND is_available = TRUE AND interval_seconds > 0
RETURNING *;
