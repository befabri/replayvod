-- name: CreateRecordingWebhookDelivery :one
INSERT INTO recording_webhook_deliveries (
    message_id, dedupe_key, event, video_id, test, next_attempt_at
)
VALUES (
    @message_id::text,
    @dedupe_key::text,
    @event::text,
    @video_id::bigint,
    @test::boolean,
    @next_attempt_at::timestamptz
)
ON CONFLICT (dedupe_key) DO UPDATE
SET dedupe_key = EXCLUDED.dedupe_key
RETURNING *;

-- name: CreateClaimedRecordingWebhookDelivery :one
-- Insert a delivery already CLAIMED (status 'delivering', one attempt counted)
-- for the synchronous send path (SendTest), which POSTs the row itself right
-- after creating it. Because the row is not 'pending', ClaimDueRecordingWebhook-
-- Delivery never selects it, so the poller cannot also deliver it — no
-- double-send. If the caller crashes mid-send, ResetStaleRecordingWebhook-
-- Deliveries returns it to 'pending' and the poller then delivers it once.
INSERT INTO recording_webhook_deliveries (
    message_id, dedupe_key, event, video_id, test, status, attempts, next_attempt_at, last_attempt_at
)
VALUES (
    @message_id::text,
    @dedupe_key::text,
    @event::text,
    @video_id::bigint,
    @test::boolean,
    'delivering',
    1,
    @next_attempt_at::timestamptz,
    @next_attempt_at::timestamptz
)
ON CONFLICT (dedupe_key) DO UPDATE
SET dedupe_key = EXCLUDED.dedupe_key
RETURNING *;

-- name: CreateRecordingWebhookDeliveryIfEnabled :one
INSERT INTO recording_webhook_deliveries (
    message_id, dedupe_key, event, video_id, test, next_attempt_at
)
SELECT
    @message_id::text,
    @dedupe_key::text,
    @event::text,
    @video_id::bigint,
    FALSE,
    @next_attempt_at::timestamptz
FROM server_settings
WHERE id = 1
  AND recording_webhook_enabled
  AND recording_webhook_url <> ''
  AND recording_webhook_secret <> ''
  AND (
    recording_webhook_events = ''
    OR @event::text = ANY(string_to_array(recording_webhook_events, ','))
  )
ON CONFLICT (dedupe_key) DO UPDATE
SET dedupe_key = EXCLUDED.dedupe_key
RETURNING *;

-- name: ClaimDueRecordingWebhookDelivery :one
UPDATE recording_webhook_deliveries rwd
SET status = 'delivering',
    attempts = attempts + 1,
    last_attempt_at = @now::timestamptz,
    updated_at = @now::timestamptz
WHERE rwd.id = (
    SELECT id
    FROM recording_webhook_deliveries
    WHERE status = 'pending' AND next_attempt_at <= @now::timestamptz
    ORDER BY next_attempt_at ASC, id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING rwd.*;

-- name: MarkRecordingWebhookDeliveryDelivered :exec
UPDATE recording_webhook_deliveries
SET status = 'delivered',
    last_status = @last_status::int,
    last_error = '',
    delivered_at = @now::timestamptz,
    updated_at = @now::timestamptz
WHERE id = @id::bigint;

-- name: MarkRecordingWebhookDeliveryFinal :exec
UPDATE recording_webhook_deliveries
SET status = @status::text,
    last_status = @last_status::int,
    last_error = @last_error::text,
    next_attempt_at = @next_attempt_at::timestamptz,
    updated_at = @now::timestamptz
WHERE id = @id::bigint;

-- name: ResetStaleRecordingWebhookDeliveries :exec
UPDATE recording_webhook_deliveries
SET status = 'pending',
    next_attempt_at = @now::timestamptz,
    updated_at = @now::timestamptz
WHERE status = 'delivering' AND updated_at < @before::timestamptz;

-- name: RetryRecordingWebhookDelivery :one
-- Manual re-queue of a terminal delivery. Constrained to failed/rejected: a
-- pending row is already queued, and resetting a delivered or delivering row
-- would cause a duplicate send. attempts is reset so the retry gets a fresh
-- budget. A non-matching id (missing, or not in a retryable state) returns no
-- row, which the adapter maps to ErrNotFound so the API can 404.
UPDATE recording_webhook_deliveries
SET status = 'pending',
    attempts = 0,
    last_status = 0,
    last_error = '',
    next_attempt_at = @now::timestamptz,
    delivered_at = NULL,
    updated_at = @now::timestamptz
WHERE id = @id::bigint
  AND status IN ('failed', 'rejected')
RETURNING *;

-- name: ListRecordingWebhookDeliveries :many
SELECT * FROM recording_webhook_deliveries
ORDER BY created_at DESC, id DESC
LIMIT @row_limit::int;

-- name: DeleteOldRecordingWebhookDeliveries :exec
-- Retention sweep: prune TERMINAL deliveries (delivered/rejected/failed) whose
-- latest terminal update is before the cutoff. pending/delivering rows are never
-- deleted regardless of age so a queued or in-flight delivery is never lost.
-- Mirrors the other log-table retention sweeps wired in the scheduler, but uses
-- updated_at instead of created_at so a long-retrying row keeps a recent outcome.
DELETE FROM recording_webhook_deliveries
WHERE updated_at < @cutoff::timestamptz
  AND status IN ('delivered', 'rejected', 'failed');

-- name: SetRecordingWebhookDeliveryFrozenParts :exec
-- Freeze the part metadata on the first delivery build, while the video's parts
-- still exist, so a later retry rebuilds the real part list even after retention
-- has deleted those parts. Signed download URLs are re-minted per attempt and
-- capped by retention, not stored here. See
-- dispatcher.bodyForDelivery.
UPDATE recording_webhook_deliveries
SET frozen_parts = @frozen_parts::text
WHERE id = @id::bigint;
