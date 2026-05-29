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
VALUES (1, $1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE
SET server_mode                       = EXCLUDED.server_mode,
    eventsub_webhook_callback_url     = EXCLUDED.eventsub_webhook_callback_url,
    eventsub_relay_ingest_url         = EXCLUDED.eventsub_relay_ingest_url,
    eventsub_relay_subscribe_url      = EXCLUDED.eventsub_relay_subscribe_url,
    eventsub_relay_local_callback_url = EXCLUDED.eventsub_relay_local_callback_url,
    updated_at                        = NOW()
RETURNING *;

-- name: GetServerHMACSecret :one
SELECT hmac_secret FROM server_settings WHERE id = 1;

-- EnsureServerHMACSecret persists secret only when none is stored yet
-- (compare-and-swap on the empty string), so concurrent boots converge on a
-- single value and an already-set secret is never overwritten. It also creates
-- the row if EventSub config has not been saved yet.
-- name: EnsureServerHMACSecret :exec
INSERT INTO server_settings (id, hmac_secret)
VALUES (1, $1)
ON CONFLICT (id) DO UPDATE
SET hmac_secret = EXCLUDED.hmac_secret,
    updated_at  = NOW()
WHERE server_settings.hmac_secret = '';
