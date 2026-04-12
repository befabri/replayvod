CREATE TABLE IF NOT EXISTS categories (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    box_art_url   TEXT,
    igdb_id       TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_categories_name ON categories (name);
