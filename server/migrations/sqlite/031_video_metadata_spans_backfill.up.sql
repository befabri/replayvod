CREATE TABLE IF NOT EXISTS video_title_spans (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id          INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    title_id          INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    started_at        TEXT NOT NULL,
    ended_at          TEXT,
    duration_seconds  REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_video_title_spans_video_id_started_at
    ON video_title_spans (video_id, started_at);

CREATE INDEX IF NOT EXISTS idx_video_title_spans_video_id_open
    ON video_title_spans (video_id, ended_at);

CREATE TABLE IF NOT EXISTS video_category_spans (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id          INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    category_id       TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    started_at        TEXT NOT NULL,
    ended_at          TEXT,
    duration_seconds  REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_video_category_spans_video_id_started_at
    ON video_category_spans (video_id, started_at);

CREATE INDEX IF NOT EXISTS idx_video_category_spans_video_id_open
    ON video_category_spans (video_id, ended_at);
