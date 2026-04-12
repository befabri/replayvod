-- webhook_events is the audit log of every EventSub webhook Twitch delivers
-- to us. Every message type (notification, verification, revocation) is
-- recorded so the dashboard can show "what happened on this channel on
-- this day" without needing to replay Twitch history.
--
-- Idempotency: Twitch retries on delivery failure, same Message-Id. The
-- UNIQUE constraint on event_id + INSERT ... ON CONFLICT DO NOTHING in
-- the handler path dedups safely.
--
-- State machine: received → processed | failed. The status column lets the
-- dashboard surface stuck-in-received rows (handler crashed mid-processing)
-- and lets a future retry job find them.
--
-- Payload retention: the scheduler (Phase 6) trims the payload column on
-- rows older than webhook_event_payload_retention_days (config, default 7).
-- Setting the config to -1 disables pruning (useful in dev/investigation);
-- 0 disables storing the payload at all (the handler still records the
-- event row for audit, just writes NULL to payload).
CREATE TABLE IF NOT EXISTS webhook_events (
    id                  BIGSERIAL PRIMARY KEY,
    event_id            TEXT NOT NULL UNIQUE,           -- Twitch-Eventsub-Message-Id (retry dedup key)
    message_type        TEXT NOT NULL                   -- Twitch-Eventsub-Message-Type
                        CHECK (message_type IN ('notification', 'webhook_callback_verification', 'revocation')),
    event_type          TEXT,                           -- subscription.type (e.g., stream.online); NULL for verification

    -- Subscription attribution. ON DELETE SET NULL so an old event record
    -- survives a subscription being hard-deleted (we soft-delete today,
    -- but the schema should tolerate hard-delete for future consideration).
    subscription_id     TEXT REFERENCES subscriptions(id) ON DELETE SET NULL,
    broadcaster_id      TEXT,                           -- denorm from payload for indexed lookup

    -- Twitch-Eventsub-Message-Timestamp header, used for replay-attack
    -- detection: Twitch requires rejecting events with a timestamp >10 min
    -- from now. We persist it so that rejection logic has the original
    -- value for audit, not just our received_at.
    message_timestamp   TIMESTAMPTZ NOT NULL,

    -- Full event body. Trimmed by scheduler per retention config.
    payload             JSONB,

    -- Lifecycle state: received on insert → processed on successful
    -- dispatch → failed on handler error. 'received' stuck for more than
    -- a few minutes indicates a crash during processing — the dashboard
    -- surfaces these for manual inspection.
    status              TEXT NOT NULL DEFAULT 'received'
                        CHECK (status IN ('received', 'processed', 'failed')),
    error               TEXT,                           -- handler error message when status='failed'

    received_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_received_at ON webhook_events (received_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_events_broadcaster ON webhook_events (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_webhook_events_type ON webhook_events (event_type);
CREATE INDEX IF NOT EXISTS idx_webhook_events_subscription ON webhook_events (subscription_id);
-- Partial index for the dashboard's "stuck events" query: status='received'
-- rows older than a threshold. Small set, highly selective.
CREATE INDEX IF NOT EXISTS idx_webhook_events_received_status
    ON webhook_events (received_at)
    WHERE status = 'received';
