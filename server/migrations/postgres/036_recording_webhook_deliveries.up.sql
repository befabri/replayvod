CREATE TABLE IF NOT EXISTS recording_webhook_deliveries (
    id              BIGSERIAL PRIMARY KEY,
    message_id      TEXT NOT NULL UNIQUE,
    dedupe_key      TEXT NOT NULL UNIQUE,
    event           TEXT NOT NULL
                    CHECK (event IN ('recording.completed', 'recording.failed', 'recording.test')),
    video_id        BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'delivering', 'delivered', 'rejected', 'failed')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_status     INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    test            BOOLEAN NOT NULL DEFAULT FALSE,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_attempt_at TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recording_webhook_deliveries_due
    ON recording_webhook_deliveries (next_attempt_at, id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_recording_webhook_deliveries_created
    ON recording_webhook_deliveries (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_recording_webhook_deliveries_status
    ON recording_webhook_deliveries (status);
