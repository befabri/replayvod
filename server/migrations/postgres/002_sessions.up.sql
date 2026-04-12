CREATE TABLE IF NOT EXISTS sessions (
    hashed_id          TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    encrypted_tokens   BYTEA NOT NULL,
    expires_at         TIMESTAMPTZ NOT NULL,
    last_active_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_agent         TEXT,
    ip_address         TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);
