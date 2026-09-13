-- name: ClaimTask :execrows
UPDATE tasks SET execution_id = ?2, last_status = 'running', last_run_at = datetime('now'),
    next_run_at = NULL, last_error = NULL, updated_at = datetime('now')
WHERE name = ?1 AND execution_id <> ?2 AND is_available = 1 AND is_enabled = 1 AND (interval_seconds > 0 OR next_run_at IS NOT NULL)
  AND last_status <> 'running'
  AND (next_run_at IS NULL OR next_run_at <= datetime('now'));

-- name: SettleTask :execrows
UPDATE tasks SET last_status = ?3, last_duration_ms = ?4, last_error = NULLIF(?5, ''),
    next_run_at = CASE
      WHEN ?3 = 'interrupted' THEN ifnull(next_run_at, datetime('now'))
      WHEN interval_seconds > 0 THEN ifnull(next_run_at, datetime('now', '+' || interval_seconds || ' seconds'))
      ELSE next_run_at END,
    updated_at = datetime('now')
WHERE name = ?1 AND execution_id = ?2 AND last_status = 'running';

-- name: ResetTaskAvailability :exec
UPDATE tasks SET is_available = 0;

-- name: RecoverInterruptedTasks :exec
-- Startup owns abandoned executions, including paused or unavailable one-shots.
UPDATE tasks SET last_status = 'interrupted',
    next_run_at = ifnull(next_run_at, datetime('now')), updated_at = datetime('now')
WHERE last_status = 'running';
