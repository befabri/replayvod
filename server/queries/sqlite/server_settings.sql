-- name: GetServerSettings :one
SELECT * FROM server_settings WHERE id = 1;

-- name: UpsertServerSettings :one
INSERT INTO server_settings (
    id,
    server_mode,
    eventsub_webhook_callback_url,
    eventsub_relay_ingest_url,
    eventsub_relay_subscribe_url,
    eventsub_relay_local_callback_url
)
VALUES (1, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE
SET server_mode                       = excluded.server_mode,
    eventsub_webhook_callback_url     = excluded.eventsub_webhook_callback_url,
    eventsub_relay_ingest_url         = excluded.eventsub_relay_ingest_url,
    eventsub_relay_subscribe_url      = excluded.eventsub_relay_subscribe_url,
    eventsub_relay_local_callback_url = excluded.eventsub_relay_local_callback_url,
    updated_at                        = datetime('now')
RETURNING *;

-- UpsertRecordingWebhookConfig writes ONLY the recording-webhook config columns
-- (enabled, url, events), leaving server_mode, the EventSub URLs, hmac_secret,
-- AND the recording webhook secret untouched. The secret is managed by its own
-- two queries below so this config write can never clobber, truncate, or race
-- the signing key. Mirrors EnsureServerHMACSecret's single-concern style.
-- name: UpsertRecordingWebhookConfig :one
INSERT INTO server_settings (
    id,
    recording_webhook_enabled,
    recording_webhook_url,
    recording_webhook_events
)
VALUES (1, ?, ?, ?)
ON CONFLICT (id) DO UPDATE
SET recording_webhook_enabled = excluded.recording_webhook_enabled,
    recording_webhook_url     = excluded.recording_webhook_url,
    recording_webhook_events  = excluded.recording_webhook_events,
    updated_at                = datetime('now')
RETURNING *;

-- EnsureRecordingWebhookSecret seeds the signing secret only when none is stored
-- yet (compare-and-swap on the empty string), exactly like EnsureServerHMACSecret.
-- The config service calls it when the webhook is first enabled, so an
-- already-set key is never overwritten and concurrent saves converge.
-- name: EnsureRecordingWebhookSecret :exec
INSERT INTO server_settings (id, recording_webhook_secret)
VALUES (1, ?)
ON CONFLICT (id) DO UPDATE
SET recording_webhook_secret = excluded.recording_webhook_secret,
    updated_at               = datetime('now')
WHERE recording_webhook_secret = '';

-- SetRecordingWebhookSecret rotates the signing secret unconditionally, for the
-- owner's explicit "regenerate secret" action.
-- name: SetRecordingWebhookSecret :exec
INSERT INTO server_settings (id, recording_webhook_secret)
VALUES (1, ?)
ON CONFLICT (id) DO UPDATE
SET recording_webhook_secret = excluded.recording_webhook_secret,
    updated_at               = datetime('now');

-- name: GetServerHMACSecret :one
SELECT hmac_secret FROM server_settings WHERE id = 1;

-- EnsureServerHMACSecret persists secret only when none is stored yet
-- (compare-and-swap on the empty string), so concurrent boots converge on a
-- single value and an already-set secret is never overwritten. It also creates
-- the row if EventSub config has not been saved yet.
-- name: EnsureServerHMACSecret :exec
INSERT INTO server_settings (id, hmac_secret)
VALUES (1, ?)
ON CONFLICT (id) DO UPDATE
SET hmac_secret = excluded.hmac_secret,
    updated_at  = datetime('now')
WHERE hmac_secret = '';
