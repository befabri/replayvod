CREATE TABLE twitch_playback_sessions (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    twitch_user_id TEXT NOT NULL,
    twitch_login TEXT NOT NULL,
    encrypted_token BYTEA NOT NULL,
    expires_at BIGINT NOT NULL DEFAULT 0,
    checked_at BIGINT NOT NULL,
    needs_reconnect BOOLEAN NOT NULL DEFAULT FALSE
);
