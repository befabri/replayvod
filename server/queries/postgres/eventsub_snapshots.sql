-- name: CreateSnapshot :one
INSERT INTO eventsub_snapshots (total, total_cost, max_total_cost)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetLatestSnapshot :one
SELECT * FROM eventsub_snapshots ORDER BY fetched_at DESC LIMIT 1;

-- name: ListSnapshots :many
SELECT * FROM eventsub_snapshots
ORDER BY fetched_at DESC
LIMIT $1 OFFSET $2;

-- name: DeleteOldSnapshots :exec
-- Retention: the dashboard chart wants maybe 30 days of history; older
-- snapshots get pruned by the scheduler task.
DELETE FROM eventsub_snapshots WHERE fetched_at < $1;

-- name: LinkSnapshotSubscription :exec
-- Called once per (snapshot, subscription) pair when the EventSub poller
-- records a snapshot. cost_at_snapshot and status_at_snapshot freeze the
-- subscription's state at snapshot time so historical queries don't
-- silently return the CURRENT values after a status/cost change.
INSERT INTO snapshot_subscriptions (snapshot_id, subscription_id, cost_at_snapshot, status_at_snapshot)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;
