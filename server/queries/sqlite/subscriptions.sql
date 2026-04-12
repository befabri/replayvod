-- name: CreateSubscription :one
INSERT INTO subscriptions (
    id, status, type, version, cost,
    condition, broadcaster_id,
    transport_method, transport_callback,
    twitch_created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = ?;

-- name: GetActiveSubscriptionForBroadcasterType :one
SELECT * FROM subscriptions
WHERE broadcaster_id = ? AND type = ? AND revoked_at IS NULL;

-- name: ListActiveSubscriptions :many
SELECT * FROM subscriptions
WHERE revoked_at IS NULL
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListSubscriptionsByBroadcaster :many
SELECT * FROM subscriptions
WHERE broadcaster_id = ?
ORDER BY created_at DESC;

-- name: ListSubscriptionsByType :many
SELECT * FROM subscriptions
WHERE type = ? AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateSubscriptionStatus :exec
UPDATE subscriptions SET status = ? WHERE id = ?;

-- name: MarkSubscriptionRevoked :exec
UPDATE subscriptions
SET revoked_at = datetime('now'), revoked_reason = ?, status = 'revoked'
WHERE id = ? AND revoked_at IS NULL;

-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = ?;

-- name: CountActiveSubscriptions :one
SELECT COUNT(*) FROM subscriptions WHERE revoked_at IS NULL;
