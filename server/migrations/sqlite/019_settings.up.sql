-- See postgres/019_settings.up.sql for design comments.
CREATE TABLE IF NOT EXISTS settings (
    user_id          TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    timezone         TEXT NOT NULL DEFAULT 'UTC',
    datetime_format  TEXT NOT NULL DEFAULT 'ISO',
    language         TEXT NOT NULL DEFAULT 'en',
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
);
