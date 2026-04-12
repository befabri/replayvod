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

-- name: ListSubscriptionsForSnapshot :many
SELECT ss.*, s.type, s.broadcaster_id
FROM snapshot_subscriptions ss
INNER JOIN subscriptions s ON s.id = ss.subscription_id
WHERE ss.snapshot_id = ?
ORDER BY s.type, s.broadcaster_id;
