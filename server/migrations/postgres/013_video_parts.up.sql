CREATE TABLE IF NOT EXISTS video_parts (
    id                  BIGSERIAL PRIMARY KEY,
    video_id            BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    part_index          INTEGER NOT NULL,
    filename            TEXT NOT NULL,
    quality             TEXT NOT NULL,
    codec               TEXT NOT NULL
                        CHECK (codec IN ('h264', 'h265', 'av1')),
    segment_format      TEXT NOT NULL
                        CHECK (segment_format IN ('ts', 'fmp4')),
    duration_seconds    DOUBLE PRECISION NOT NULL DEFAULT 0,
    size_bytes          BIGINT NOT NULL DEFAULT 0,
    thumbnail           TEXT,
    start_media_seq     BIGINT NOT NULL,
    end_media_seq       BIGINT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (video_id, part_index)
);

CREATE INDEX IF NOT EXISTS idx_video_parts_video_id ON video_parts (video_id);
