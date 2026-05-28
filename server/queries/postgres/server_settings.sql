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
