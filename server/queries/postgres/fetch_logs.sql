-- name: CreateFetchLog :exec
INSERT INTO fetch_logs (user_id, fetch_type, broadcaster_id, status, error, duration_ms)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListFetchLogs :many
SELECT * FROM fetch_logs ORDER BY fetched_at DESC LIMIT $1 OFFSET $2;

-- name: ListFetchLogsByType :many
SELECT * FROM fetch_logs WHERE fetch_type = $1 ORDER BY fetched_at DESC LIMIT $2 OFFSET $3;

-- name: CountFetchLogs :one
SELECT COUNT(*) FROM fetch_logs;

-- name: CountFetchLogsByType :one
SELECT COUNT(*) FROM fetch_logs WHERE fetch_type = $1;

-- name: DeleteOldFetchLogs :exec
DELETE FROM fetch_logs WHERE fetched_at < $1;
