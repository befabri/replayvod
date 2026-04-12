-- See postgres/015_webhook_events.up.sql for full design comments.
CREATE TABLE IF NOT EXISTS webhook_events (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id            TEXT NOT NULL UNIQUE,
    message_type        TEXT NOT NULL
                        CHECK (message_type IN ('notification', 'webhook_callback_verification', 'revocation')),
    event_type          TEXT,
    subscription_id     TEXT REFERENCES subscriptions(id) ON DELETE SET NULL,
    broadcaster_id      TEXT,
    message_timestamp   TEXT NOT NULL,
    payload             TEXT,                           -- JSON, retained per webhook_event_payload_retention_days
    status              TEXT NOT NULL DEFAULT 'received'
                        CHECK (status IN ('received', 'processed', 'failed')),
    error               TEXT,
    received_at         TEXT NOT NULL DEFAULT (datetime('now')),
    processed_at        TEXT
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_received_at ON webhook_events (received_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_events_broadcaster ON webhook_events (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_webhook_events_type ON webhook_events (event_type);
CREATE INDEX IF NOT EXISTS idx_webhook_events_subscription ON webhook_events (subscription_id);
CREATE INDEX IF NOT EXISTS idx_webhook_events_received_status
    ON webhook_events (received_at)
    WHERE status = 'received';
