-- name: CreateWebhookEvent :one
INSERT INTO webhook_events (
    event_id, message_type, event_type,
    subscription_id, broadcaster_id,
    message_timestamp, payload
)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (event_id) DO NOTHING
RETURNING *;

-- name: GetWebhookEvent :one
SELECT * FROM webhook_events WHERE id = ?;

-- name: GetWebhookEventByEventID :one
SELECT * FROM webhook_events WHERE event_id = ?;

-- name: MarkWebhookEventProcessed :exec
UPDATE webhook_events
SET status = 'processed', processed_at = datetime('now'), error = NULL
WHERE id = ?;

-- name: MarkWebhookEventFailed :exec
UPDATE webhook_events
SET status = 'failed', processed_at = datetime('now'), error = ?
WHERE id = ?;

-- name: ListWebhookEvents :many
SELECT * FROM webhook_events
ORDER BY received_at DESC
LIMIT ? OFFSET ?;

-- name: ListWebhookEventsByBroadcaster :many
SELECT * FROM webhook_events
WHERE broadcaster_id = ?
ORDER BY received_at DESC
LIMIT ? OFFSET ?;

-- name: ListWebhookEventsByType :many
SELECT * FROM webhook_events
WHERE event_type = ?
ORDER BY received_at DESC
LIMIT ? OFFSET ?;

-- name: ListStuckWebhookEvents :many
SELECT * FROM webhook_events
WHERE status = 'received' AND received_at < ?
ORDER BY received_at DESC
LIMIT ?;

-- name: ClearWebhookEventPayload :exec
UPDATE webhook_events
SET payload = NULL
WHERE received_at < ? AND payload IS NOT NULL;

-- name: CountWebhookEvents :one
SELECT COUNT(*) FROM webhook_events;

-- name: CountWebhookEventsByType :one
SELECT COUNT(*) FROM webhook_events WHERE event_type = ?;
