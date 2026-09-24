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

-- name: GetWebhookEventByEventID :one
SELECT * FROM webhook_events WHERE event_id = $1;

-- name: MarkWebhookEventProcessed :exec
UPDATE webhook_events
SET status = 'processed', processed_at = NOW(), error = NULL
WHERE id = $1;

-- name: MarkWebhookEventFailed :exec
UPDATE webhook_events
SET status = 'failed', processed_at = NOW(), error = @err_msg
WHERE id = @id;

-- name: ClearWebhookEventPayload :exec
-- Retention trim: scheduler task nulls the payload on rows older than
-- webhook_event_payload_retention_days. The row (with audit metadata)
-- stays; just the fat JSON column goes.
UPDATE webhook_events
SET payload = NULL
WHERE received_at < $1 AND payload IS NOT NULL;

-- name: DeleteOldWebhookEvents :exec
-- Retention sweep. The handler rejects a delivery older than the replay window
-- before it dedupes, so deleting a row cannot let its event run again.
DELETE FROM webhook_events WHERE received_at < $1;

-- name: CountWebhookEvents :one
SELECT COUNT(*) FROM webhook_events;
