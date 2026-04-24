-- See postgres/030_video_metadata_durations.up.sql for the rationale
-- on the partial indexes and the open-span invariant. SQLite mirrors
-- the pg shape exactly, stored as TEXT because modernc.org/sqlite's
-- time.Time binding emits a format julianday() can't parse; the
-- adapter formats timestamps as "2006-01-02 15:04:05" strings.
CREATE TABLE video_title_spans (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id          INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    title_id          INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    started_at        TEXT NOT NULL,
    ended_at          TEXT,
    duration_seconds  REAL NOT NULL DEFAULT 0
);

CREATE INDEX idx_video_title_spans_video_id_started_at
    ON video_title_spans (video_id, started_at);

CREATE INDEX idx_video_title_spans_video_id_open
    ON video_title_spans (video_id)
    WHERE ended_at IS NULL;

CREATE UNIQUE INDEX idx_video_title_spans_unique_open
    ON video_title_spans (video_id, title_id)
    WHERE ended_at IS NULL;

CREATE TABLE video_category_spans (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id          INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    category_id       TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    started_at        TEXT NOT NULL,
    ended_at          TEXT,
    duration_seconds  REAL NOT NULL DEFAULT 0
);

CREATE INDEX idx_video_category_spans_video_id_started_at
    ON video_category_spans (video_id, started_at);

CREATE INDEX idx_video_category_spans_video_id_open
    ON video_category_spans (video_id)
    WHERE ended_at IS NULL;

CREATE UNIQUE INDEX idx_video_category_spans_unique_open
    ON video_category_spans (video_id, category_id)
    WHERE ended_at IS NULL;
