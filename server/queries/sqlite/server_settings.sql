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
