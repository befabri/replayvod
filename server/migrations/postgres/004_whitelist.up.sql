CREATE TABLE IF NOT EXISTS whitelist (
    twitch_user_id  TEXT PRIMARY KEY,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
