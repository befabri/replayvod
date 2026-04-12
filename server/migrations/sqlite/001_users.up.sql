CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    login           TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    email           TEXT,
    profile_image_url TEXT,
    role            TEXT NOT NULL DEFAULT 'viewer',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_users_login ON users (login);
