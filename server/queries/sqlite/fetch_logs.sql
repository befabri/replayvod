-- name: CreateFetchLog :exec
INSERT INTO fetch_logs (user_id, fetch_type, broadcaster_id, status, error, duration_ms)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListFetchLogs :many
SELECT * FROM fetch_logs ORDER BY fetched_at DESC LIMIT ? OFFSET ?;

-- name: ListFetchLogsByType :many
SELECT * FROM fetch_logs WHERE fetch_type = ? ORDER BY fetched_at DESC LIMIT ? OFFSET ?;

-- name: CountFetchLogs :one
SELECT COUNT(*) FROM fetch_logs;

-- name: CountFetchLogsByType :one
SELECT COUNT(*) FROM fetch_logs WHERE fetch_type = ?;

-- name: DeleteOldFetchLogs :exec
DELETE FROM fetch_logs WHERE fetched_at < ?;
