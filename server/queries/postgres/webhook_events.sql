-- name: CreateWebhookEvent :one
-- Idempotent: Twitch retries with the same Message-Id on delivery failure.
-- ON CONFLICT DO NOTHING avoids double-processing; the RETURNING is NULL
-- on conflict so the handler knows the event was already recorded.
INSERT INTO webhook_events (
    event_id, message_type, event_type,
    subscription_id, broadcaster_id,
    message_timestamp, payload
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (event_id) DO NOTHING
RETURNING *;

-- name: GetWebhookEvent :one
SELECT * FROM webhook_events WHERE id = $1;

-- name: GetWebhookEventByEventID :one
SELECT * FROM webhook_events WHERE event_id = $1;

-- name: MarkWebhookEventProcessed :exec
UPDATE webhook_events
SET status = 'processed', processed_at = NOW(), error = NULL
WHERE id = $1;

-- name: MarkWebhookEventFailed :exec
UPDATE webhook_events
SET status = 'failed', processed_at = NOW(), error = $2
WHERE id = $1;

-- name: ListWebhookEvents :many
SELECT * FROM webhook_events
ORDER BY received_at DESC
LIMIT $1 OFFSET $2;

-- name: ListWebhookEventsByBroadcaster :many
SELECT * FROM webhook_events
WHERE broadcaster_id = $1
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- name: ListWebhookEventsByType :many
SELECT * FROM webhook_events
WHERE event_type = $1
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- name: ListStuckWebhookEvents :many
-- Dashboard "stuck" query: status='received' rows older than a threshold
-- indicate the handler crashed mid-processing. Partial index
-- idx_webhook_events_received_status keeps this fast.
SELECT * FROM webhook_events
WHERE status = 'received' AND received_at < $1
ORDER BY received_at DESC
LIMIT $2;

-- name: ClearWebhookEventPayload :exec
-- Retention trim: scheduler task nulls the payload on rows older than
-- webhook_event_payload_retention_days. The row (with audit metadata)
-- stays; just the fat JSON column goes.
UPDATE webhook_events
SET payload = NULL
WHERE received_at < $1 AND payload IS NOT NULL;

-- name: CountWebhookEvents :one
SELECT COUNT(*) FROM webhook_events;

-- name: CountWebhookEventsByType :one
SELECT COUNT(*) FROM webhook_events WHERE event_type = $1;
