-- name: CreateEventLog :one
INSERT INTO event_logs (domain, event_type, severity, message, actor_user_id, data)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListEventLogs :many
SELECT * FROM event_logs
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListEventLogsByDomain :many
SELECT * FROM event_logs
WHERE domain = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListEventLogsBySeverity :many
SELECT * FROM event_logs
WHERE severity = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountEventLogs :one
SELECT COUNT(*) FROM event_logs;

-- name: CountEventLogsByDomain :one
SELECT COUNT(*) FROM event_logs WHERE domain = $1;

-- name: DeleteOldEventLogs :exec
-- Retention task path: debug/info rows older than the retention window
-- are pruned; warn/error rows stay longer (retention task uses a
-- different cutoff). Partial WHERE keeps the sweep focused.
DELETE FROM event_logs
WHERE created_at < $1
  AND severity IN ('debug', 'info');
