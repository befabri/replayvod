CREATE TABLE IF NOT EXISTS sessions (
    hashed_id          TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    encrypted_tokens   BLOB NOT NULL,
    expires_at         TEXT NOT NULL,
    last_active_at     TEXT NOT NULL DEFAULT (datetime('now')),
    user_agent         TEXT,
    ip_address         TEXT,
    created_at         TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);
