CREATE TABLE IF NOT EXISTS fetch_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         TEXT REFERENCES users(id) ON DELETE SET NULL,
    fetch_type      TEXT NOT NULL,
    broadcaster_id  TEXT,
    status          INTEGER NOT NULL,
    error           TEXT,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    fetched_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_fetch_logs_fetched_at ON fetch_logs (fetched_at DESC);
CREATE INDEX IF NOT EXISTS idx_fetch_logs_user_id ON fetch_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_fetch_logs_type ON fetch_logs (fetch_type);
