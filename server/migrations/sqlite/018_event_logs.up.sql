-- See postgres/018_event_logs.up.sql for design comments.
CREATE TABLE IF NOT EXISTS event_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    domain          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'info'
                    CHECK (severity IN ('debug', 'info', 'warn', 'error')),
    message         TEXT NOT NULL,
    actor_user_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
    data            TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_event_logs_created_at ON event_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_event_logs_domain_type ON event_logs (domain, event_type);
CREATE INDEX IF NOT EXISTS idx_event_logs_severity ON event_logs (severity) WHERE severity IN ('warn', 'error');
