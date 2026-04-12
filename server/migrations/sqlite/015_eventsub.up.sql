-- See postgres/014_eventsub.up.sql for detailed comments on design intent.
-- SQLite mirror uses TEXT for timestamps and JSONB → TEXT (the condition
-- and snapshot-subscriptions are stored as JSON strings; SQLite's JSON1
-- extension can query into them).
CREATE TABLE IF NOT EXISTS subscriptions (
    id                  TEXT PRIMARY KEY,
    status              TEXT NOT NULL,
    type                TEXT NOT NULL,
    version             TEXT NOT NULL,
    cost                INTEGER NOT NULL DEFAULT 0,
    condition           TEXT NOT NULL,                  -- JSON, queried via json_extract when needed
    broadcaster_id      TEXT REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    transport_method    TEXT NOT NULL,
    transport_callback  TEXT NOT NULL,
    twitch_created_at   TEXT NOT NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    revoked_at          TEXT,
    revoked_reason      TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_active_unique
    ON subscriptions (broadcaster_id, type)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_subscriptions_broadcaster ON subscriptions (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions (status);
CREATE INDEX IF NOT EXISTS idx_subscriptions_type ON subscriptions (type);

CREATE TABLE IF NOT EXISTS eventsub_snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    total           INTEGER NOT NULL DEFAULT 0,
    total_cost      INTEGER NOT NULL DEFAULT 0,
    max_total_cost  INTEGER NOT NULL DEFAULT 0,
    fetched_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_eventsub_snapshots_fetched_at
    ON eventsub_snapshots (fetched_at DESC);

CREATE TABLE IF NOT EXISTS snapshot_subscriptions (
    snapshot_id         INTEGER NOT NULL REFERENCES eventsub_snapshots(id) ON DELETE CASCADE,
    subscription_id     TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    cost_at_snapshot    INTEGER NOT NULL,
    status_at_snapshot  TEXT NOT NULL,
    PRIMARY KEY (snapshot_id, subscription_id)
);

CREATE INDEX IF NOT EXISTS idx_snapshot_subs_subscription ON snapshot_subscriptions (subscription_id);
