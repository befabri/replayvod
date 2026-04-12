CREATE TABLE IF NOT EXISTS categories (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    box_art_url   TEXT,
    igdb_id       TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_categories_name ON categories (name);
