-- name: UpsertTask :one
INSERT INTO tasks (name, description, interval_seconds)
VALUES (?, ?, ?)
ON CONFLICT (name) DO UPDATE
SET description      = excluded.description,
    interval_seconds = excluded.interval_seconds,
    updated_at       = datetime('now')
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE name = ?;

-- name: ListTasks :many
SELECT * FROM tasks ORDER BY name;

-- name: ListDueTasks :many
SELECT * FROM tasks
WHERE is_enabled = 1
  AND interval_seconds > 0
  AND (next_run_at IS NULL OR next_run_at <= datetime('now'))
ORDER BY CASE WHEN next_run_at IS NULL THEN 0 ELSE 1 END, next_run_at;

-- name: MarkTaskRunning :exec
UPDATE tasks
SET last_status = 'running',
    last_run_at = datetime('now'),
    last_error  = NULL,
    updated_at  = datetime('now')
WHERE name = ?;

-- name: MarkTaskSuccess :exec
UPDATE tasks
SET last_status      = 'success',
    last_duration_ms = ?2,
    next_run_at      = CASE
        WHEN interval_seconds > 0
        THEN datetime('now', '+' || interval_seconds || ' seconds')
        ELSE NULL
    END,
    last_error       = NULL,
    updated_at       = datetime('now')
WHERE name = ?1;

-- name: MarkTaskFailed :exec
UPDATE tasks
SET last_status      = 'failed',
    last_duration_ms = ?2,
    last_error       = ?3,
    next_run_at      = CASE
        WHEN interval_seconds > 0
        THEN datetime('now', '+' || interval_seconds || ' seconds')
        ELSE NULL
    END,
    updated_at       = datetime('now')
WHERE name = ?1;

-- name: SetTaskEnabled :one
UPDATE tasks
SET is_enabled  = ?2,
    next_run_at = CASE
        WHEN ?2 = 1 AND interval_seconds > 0 AND next_run_at IS NULL
        THEN datetime('now')
        ELSE next_run_at
    END,
    updated_at  = datetime('now')
WHERE name = ?1
RETURNING *;

-- name: SetTaskNextRun :exec
UPDATE tasks
SET next_run_at = datetime('now'),
    updated_at  = datetime('now')
WHERE name = ?;
