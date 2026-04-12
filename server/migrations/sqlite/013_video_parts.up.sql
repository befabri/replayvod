CREATE TABLE IF NOT EXISTS video_parts (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id            INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    part_index          INTEGER NOT NULL,
    filename            TEXT NOT NULL,
    quality             TEXT NOT NULL,
    codec               TEXT NOT NULL
                        CHECK (codec IN ('h264', 'h265', 'av1')),
    segment_format      TEXT NOT NULL
                        CHECK (segment_format IN ('ts', 'fmp4')),
    duration_seconds    REAL NOT NULL DEFAULT 0,
    size_bytes          INTEGER NOT NULL DEFAULT 0,
    thumbnail           TEXT,
    start_media_seq     INTEGER NOT NULL,
    end_media_seq       INTEGER NOT NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),

    UNIQUE (video_id, part_index)
);

CREATE INDEX IF NOT EXISTS idx_video_parts_video_id ON video_parts (video_id);
