-- server_settings stores process-wide settings that are configured through the
-- owner UI rather than through environment variables. It is intentionally
-- separate from the existing per-user settings table, which stores dashboard
-- display preferences keyed by user_id.
--
-- A single-row table keeps EventSub configuration typed and migratable without
-- turning settings into an unstructured key/value bag. Future server settings
-- can add columns here when they need validation, redaction, or boot-time use.
CREATE TABLE IF NOT EXISTS server_settings (
    id                                  SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),

    -- Empty means "not configured in the app yet"; the UI should show setup.
    -- Valid non-empty values mirror SERVER_MODE: off, poll, direct, relay.
    server_mode                         TEXT NOT NULL DEFAULT '',

    -- Used only when server_mode = 'direct'.
    eventsub_webhook_callback_url       TEXT NOT NULL DEFAULT '',

    -- Used only when server_mode = 'relay'.
    eventsub_relay_ingest_url           TEXT NOT NULL DEFAULT '',
    eventsub_relay_subscribe_url        TEXT NOT NULL DEFAULT '',
    eventsub_relay_local_callback_url   TEXT NOT NULL DEFAULT '',

    created_at                          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
