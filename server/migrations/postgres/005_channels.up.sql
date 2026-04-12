CREATE TABLE IF NOT EXISTS channels (
    broadcaster_id      TEXT PRIMARY KEY,
    broadcaster_login   TEXT NOT NULL,
    broadcaster_name    TEXT NOT NULL,
    broadcaster_language TEXT,
    profile_image_url   TEXT,
    offline_image_url   TEXT,
    description         TEXT,
    broadcaster_type    TEXT,
    view_count          INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_channels_login ON channels (broadcaster_login);

CREATE TABLE IF NOT EXISTS user_followed_channels (
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    broadcaster_id  TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    followed_at     TIMESTAMPTZ NOT NULL,
    followed        BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (user_id, broadcaster_id)
);

CREATE INDEX IF NOT EXISTS idx_ufc_user_id ON user_followed_channels (user_id);
CREATE INDEX IF NOT EXISTS idx_ufc_broadcaster_id ON user_followed_channels (broadcaster_id);
