-- name: CreateSubscription :one
INSERT INTO subscriptions (
    id, status, type, version, cost,
    condition, broadcaster_id,
    transport_method, transport_callback,
    twitch_created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: UpsertSubscription :one
-- Self-heal path: the snapshot poll discovers a sub Twitch has that we
-- don't mirror locally (another tool created it, or our create path
-- succeeded on Twitch but failed to record). Insert the row so the
-- junction link succeeds; on conflict refresh the mutable columns so
-- a status drift Twitch surfaces is reflected locally without
-- clobbering revoked_at / revoked_reason (those belong to our
-- soft-delete lifecycle, not Twitch's).
INSERT INTO subscriptions (
    id, status, type, version, cost,
    condition, broadcaster_id,
    transport_method, transport_callback,
    twitch_created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE
SET status            = EXCLUDED.status,
    cost              = EXCLUDED.cost,
    transport_method  = EXCLUDED.transport_method,
    transport_callback = EXCLUDED.transport_callback
RETURNING *;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = $1;

-- name: GetActiveSubscriptionForBroadcasterType :one
-- Respects the partial UNIQUE: at most one active (non-revoked) sub per
-- (broadcaster_id, type). Used before creating to prevent duplicate calls
-- to Twitch that would fail with 409.
SELECT * FROM subscriptions
WHERE broadcaster_id = $1 AND type = $2 AND revoked_at IS NULL;

-- name: ListActiveSubscriptions :many
SELECT * FROM subscriptions
WHERE revoked_at IS NULL
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListSubscriptionsByBroadcaster :many
SELECT * FROM subscriptions
WHERE broadcaster_id = $1
ORDER BY created_at DESC;

-- name: ListSubscriptionsByType :many
SELECT * FROM subscriptions
WHERE type = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateSubscriptionStatus :exec
UPDATE subscriptions SET status = $2 WHERE id = $1;

-- name: MarkSubscriptionRevoked :exec
-- Soft-delete. Called when Twitch sends a revocation message or when we
-- issue a DELETE via the Helix API. Preserves the row for audit; the
-- partial UNIQUE index then allows creating a replacement subscription.
UPDATE subscriptions
SET revoked_at = NOW(), revoked_reason = $2, status = 'revoked'
WHERE id = $1 AND revoked_at IS NULL;

-- name: DeleteSubscription :exec
-- Hard-delete. Only intended for cleanup after a full system teardown or
-- rebuild; production code paths should call MarkSubscriptionRevoked.
DELETE FROM subscriptions WHERE id = $1;

-- name: CountActiveSubscriptions :one
SELECT COUNT(*) FROM subscriptions WHERE revoked_at IS NULL;
