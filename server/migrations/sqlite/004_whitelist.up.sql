CREATE TABLE IF NOT EXISTS whitelist (
    twitch_user_id  TEXT PRIMARY KEY,
    added_at        TEXT NOT NULL DEFAULT (datetime('now'))
);
