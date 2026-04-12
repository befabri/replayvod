-- Reverse of 023: drop updated_at, drop UNIQUE(filename), restore
-- end_media_seq NOT NULL. Rows with NULL end_media_seq get their
-- start_media_seq copied in so the NOT NULL reinstate succeeds;
-- those rows are un-finalized anyway and would fail the app-layer
-- invariant on the next run, so this is acceptable.
PRAGMA foreign_keys=OFF;

CREATE TABLE video_parts_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id            INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    part_index          INTEGER NOT NULL,
    filename            TEXT NOT NULL,
    quality             TEXT NOT NULL,
    codec               TEXT NOT NULL
                        CHECK (codec IN ('h264', 'h265', 'av1', 'aac')),
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

INSERT INTO video_parts_new (
    id, video_id, part_index, filename, quality, codec, segment_format,
    duration_seconds, size_bytes, thumbnail, start_media_seq, end_media_seq,
    created_at
) SELECT
    id, video_id, part_index, filename, quality, codec, segment_format,
    duration_seconds, size_bytes, thumbnail, start_media_seq,
    COALESCE(end_media_seq, start_media_seq),
    created_at
FROM video_parts;

DROP TABLE video_parts;
ALTER TABLE video_parts_new RENAME TO video_parts;

CREATE INDEX IF NOT EXISTS idx_video_parts_video_id ON video_parts (video_id);

PRAGMA foreign_keys=ON;
