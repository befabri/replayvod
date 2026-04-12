CREATE TABLE IF NOT EXISTS streams (
    id              TEXT PRIMARY KEY,
    broadcaster_id  TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    type            TEXT NOT NULL DEFAULT 'live',
    language        TEXT NOT NULL DEFAULT '',
    thumbnail_url   TEXT,
    viewer_count    INTEGER NOT NULL DEFAULT 0,
    is_mature       INTEGER,
    started_at      TEXT NOT NULL,
    ended_at        TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_streams_broadcaster_id ON streams (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_streams_started_at ON streams (started_at DESC);
CREATE INDEX IF NOT EXISTS idx_streams_active ON streams (broadcaster_id) WHERE ended_at IS NULL;
