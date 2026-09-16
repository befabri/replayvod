-- name: UpsertTask :one
INSERT INTO tasks (name, description, interval_seconds, is_available)
VALUES (?, ?, ?, 1)
ON CONFLICT (name) DO UPDATE
SET is_available     = 1,
    description      = excluded.description,
    interval_seconds = excluded.interval_seconds,
    updated_at       = datetime('now')
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE name = ?;

-- name: ListTasks :many
SELECT * FROM tasks ORDER BY name;

-- name: ListDueTasks :many
SELECT * FROM tasks
WHERE is_available = 1 AND is_enabled = 1
  AND last_status <> 'running'
  AND (interval_seconds > 0 OR next_run_at IS NOT NULL)
  AND (next_run_at IS NULL OR next_run_at <= datetime('now'))
ORDER BY CASE WHEN next_run_at IS NULL THEN 0 ELSE 1 END, next_run_at;

-- name: SetTaskEnabled :one
UPDATE tasks
SET is_enabled  = @enabled,
    next_run_at = CASE
        WHEN @enabled = 1 AND is_available = 1 AND interval_seconds > 0 AND next_run_at IS NULL
        THEN datetime('now')
        ELSE next_run_at
    END,
    updated_at  = datetime('now')
WHERE name = @name
RETURNING *;

-- name: SetTaskNextRun :one
UPDATE tasks
SET next_run_at = datetime('now'),
    updated_at  = datetime('now')
WHERE name = ? AND is_available = 1
RETURNING *;

-- name: ScheduleTaskIfEnabled :one
UPDATE tasks SET next_run_at = datetime('now'), updated_at = datetime('now')
WHERE name = ?1 AND is_enabled = 1 AND is_available = 1 AND interval_seconds > 0
RETURNING *;
