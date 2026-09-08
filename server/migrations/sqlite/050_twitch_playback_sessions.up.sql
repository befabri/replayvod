CREATE TABLE twitch_playback_sessions (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    twitch_user_id TEXT NOT NULL,
    twitch_login TEXT NOT NULL,
    encrypted_token BLOB NOT NULL,
    expires_at INTEGER NOT NULL DEFAULT 0,
    checked_at INTEGER NOT NULL,
    needs_reconnect INTEGER NOT NULL DEFAULT 0 CHECK (needs_reconnect IN (0, 1))
);
