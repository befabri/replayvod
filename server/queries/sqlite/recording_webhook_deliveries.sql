-- name: CreateRecordingWebhookDelivery :one
INSERT INTO recording_webhook_deliveries (
    message_id, dedupe_key, event, video_id, test, next_attempt_at
)
VALUES (@message_id, @dedupe_key, @event, @video_id, @test, @next_attempt_at)
ON CONFLICT (dedupe_key) DO UPDATE
SET dedupe_key = excluded.dedupe_key
RETURNING *;

-- name: CreateClaimedRecordingWebhookDelivery :one
-- Insert a delivery already CLAIMED (status 'delivering', one attempt counted)
-- for the synchronous send path (SendTest). Not 'pending', so the poller's
-- ClaimDueRecordingWebhookDelivery never selects it and cannot double-send it;
-- ResetStaleRecordingWebhookDeliveries recovers it if the caller crashes.
INSERT INTO recording_webhook_deliveries (
    message_id, dedupe_key, event, video_id, test, status, attempts, next_attempt_at, last_attempt_at
)
VALUES (@message_id, @dedupe_key, @event, @video_id, @test, 'delivering', 1, @next_attempt_at, @next_attempt_at)
ON CONFLICT (dedupe_key) DO UPDATE
SET dedupe_key = excluded.dedupe_key
RETURNING *;

-- name: CreateRecordingWebhookDeliveryIfEnabled :one
INSERT INTO recording_webhook_deliveries (
    message_id, dedupe_key, event, video_id, test, next_attempt_at
)
SELECT @message_id, @dedupe_key, @event, @video_id, 0, @next_attempt_at
FROM server_settings
WHERE id = 1
  AND recording_webhook_enabled = 1
  AND recording_webhook_url <> ''
  AND recording_webhook_secret <> ''
  AND (
    recording_webhook_events = ''
    OR instr(',' || recording_webhook_events || ',', ',' || @event || ',') > 0
  )
ON CONFLICT (dedupe_key) DO UPDATE
SET dedupe_key = excluded.dedupe_key
RETURNING *;

-- name: ClaimDueRecordingWebhookDelivery :one
UPDATE recording_webhook_deliveries
SET status = 'delivering',
    attempts = attempts + 1,
    last_attempt_at = @now,
    updated_at = @now
WHERE id = (
    SELECT id
    FROM recording_webhook_deliveries
    WHERE status = 'pending' AND next_attempt_at <= @now
    ORDER BY next_attempt_at ASC, id ASC
    LIMIT 1
)
RETURNING *;

-- name: MarkRecordingWebhookDeliveryDelivered :exec
UPDATE recording_webhook_deliveries
SET status = 'delivered',
    last_status = @last_status,
    last_error = '',
    delivered_at = @now,
    updated_at = @now
WHERE id = @id;

-- name: MarkRecordingWebhookDeliveryFinal :exec
UPDATE recording_webhook_deliveries
SET status = @status,
    last_status = @last_status,
    last_error = @last_error,
    next_attempt_at = @next_attempt_at,
    updated_at = @now
WHERE id = @id;

-- name: ResetStaleRecordingWebhookDeliveries :exec
UPDATE recording_webhook_deliveries
SET status = 'pending',
    next_attempt_at = @now,
    updated_at = @now
WHERE status = 'delivering' AND updated_at < @before;

-- name: RetryRecordingWebhookDelivery :one
-- Manual re-queue of a terminal delivery, constrained to failed/rejected (a
-- pending row is already queued; resetting a delivered/delivering row would
-- duplicate-send). attempts is reset for a fresh budget. A non-matching id
-- returns no row, which the adapter maps to ErrNotFound.
UPDATE recording_webhook_deliveries
SET status = 'pending',
    attempts = 0,
    last_status = 0,
    last_error = '',
    next_attempt_at = @now,
    delivered_at = NULL,
    updated_at = @now
WHERE id = @id
  AND status IN ('failed', 'rejected')
RETURNING *;

-- name: ListRecordingWebhookDeliveries :many
SELECT * FROM recording_webhook_deliveries
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: DeleteOldRecordingWebhookDeliveries :exec
-- Retention sweep: prune TERMINAL deliveries (delivered/rejected/failed) created
-- before the cutoff. pending/delivering rows are never deleted regardless of age
-- so a queued or in-flight delivery is never lost.
DELETE FROM recording_webhook_deliveries
WHERE created_at < @cutoff
  AND status IN ('delivered', 'rejected', 'failed');
