-- name: CreateSnapshot :one
INSERT INTO eventsub_snapshots (total, total_cost, max_total_cost)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetLatestSnapshot :one
SELECT * FROM eventsub_snapshots ORDER BY fetched_at DESC LIMIT 1;

-- name: ListSnapshots :many
SELECT * FROM eventsub_snapshots
ORDER BY fetched_at DESC
LIMIT ? OFFSET ?;

-- name: DeleteOldSnapshots :exec
DELETE FROM eventsub_snapshots WHERE fetched_at < ?;

-- name: LinkSnapshotSubscription :exec
INSERT INTO snapshot_subscriptions (snapshot_id, subscription_id, cost_at_snapshot, status_at_snapshot)
VALUES (?, ?, ?, ?)
ON CONFLICT DO NOTHING;
