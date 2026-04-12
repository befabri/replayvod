-- name: CreateEventLog :one
INSERT INTO event_logs (domain, event_type, severity, message, actor_user_id, data)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListEventLogs :many
SELECT * FROM event_logs
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListEventLogsByDomain :many
SELECT * FROM event_logs
WHERE domain = ?1
ORDER BY created_at DESC
LIMIT ?2 OFFSET ?3;

-- name: ListEventLogsBySeverity :many
SELECT * FROM event_logs
WHERE severity = ?1
ORDER BY created_at DESC
LIMIT ?2 OFFSET ?3;

-- name: CountEventLogs :one
SELECT COUNT(*) FROM event_logs;

-- name: CountEventLogsByDomain :one
SELECT COUNT(*) FROM event_logs WHERE domain = ?;

-- name: DeleteOldEventLogs :exec
DELETE FROM event_logs
WHERE created_at < ?
  AND severity IN ('debug', 'info');
