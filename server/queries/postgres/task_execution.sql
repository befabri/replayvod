-- name: ClaimTask :execrows
UPDATE tasks SET execution_id = $2, last_status = 'running', last_run_at = NOW(),
    next_run_at = NULL, last_error = NULL, updated_at = NOW()
WHERE name = $1 AND execution_id <> $2 AND is_available = TRUE AND is_enabled = TRUE AND (interval_seconds > 0 OR next_run_at IS NOT NULL)
  AND last_status <> 'running'
  AND (next_run_at IS NULL OR next_run_at <= NOW());

-- name: SettleTask :execrows
UPDATE tasks SET last_status = $3, last_duration_ms = $4, last_error = NULLIF($5, ''),
    next_run_at = CASE
      WHEN $3 = 'interrupted' THEN COALESCE(next_run_at, NOW())
      WHEN interval_seconds > 0 THEN COALESCE(next_run_at, NOW() + (interval_seconds * INTERVAL '1 second'))
      ELSE next_run_at END,
    updated_at = NOW()
WHERE name = $1 AND execution_id = $2 AND last_status = 'running';

-- name: ResetTaskAvailability :exec
UPDATE tasks SET is_available = FALSE;

-- name: RecoverInterruptedTasks :exec
-- Startup owns abandoned executions, including paused or unavailable one-shots.
UPDATE tasks SET last_status = 'interrupted',
    next_run_at = COALESCE(next_run_at, NOW()), updated_at = NOW()
WHERE last_status = 'running';
