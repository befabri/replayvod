-- name: CreateWebhookEvent :one
INSERT INTO webhook_events (
    event_id, message_type, event_type,
    subscription_id, broadcaster_id,
    message_timestamp, payload
)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (event_id) DO NOTHING
RETURNING *;

-- name: GetWebhookEventByEventID :one
SELECT * FROM webhook_events WHERE event_id = ?;

-- name: MarkWebhookEventProcessed :exec
UPDATE webhook_events
SET status = 'processed', processed_at = datetime('now'), error = NULL
WHERE id = ?;

-- name: MarkWebhookEventFailed :exec
UPDATE webhook_events
SET status = 'failed', processed_at = datetime('now'), error = @err_msg
WHERE id = @id;

-- name: ClearWebhookEventPayload :exec
UPDATE webhook_events
SET payload = NULL
WHERE received_at < ? AND payload IS NOT NULL;

-- name: DeleteOldWebhookEvents :exec
-- Retention sweep. The handler rejects a delivery older than the replay window
-- before it dedupes, so deleting a row cannot let its event run again.
DELETE FROM webhook_events WHERE received_at < ?;

-- name: CountWebhookEvents :one
SELECT COUNT(*) FROM webhook_events;
