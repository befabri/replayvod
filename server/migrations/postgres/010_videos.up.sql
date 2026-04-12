CREATE TABLE IF NOT EXISTS videos (
    id                  BIGSERIAL PRIMARY KEY,
    job_id              TEXT NOT NULL UNIQUE,
    filename            TEXT NOT NULL UNIQUE,
    display_name        TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'PENDING'
                        CHECK (status IN ('PENDING', 'RUNNING', 'DONE', 'FAILED')),
    quality             TEXT NOT NULL DEFAULT 'HIGH'
                        CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH')),
    broadcaster_id      TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    stream_id           TEXT REFERENCES streams(id) ON DELETE SET NULL,
    viewer_count        INTEGER NOT NULL DEFAULT 0,
    language            TEXT NOT NULL DEFAULT '',
    duration_seconds    DOUBLE PRECISION,
    size_bytes          BIGINT,
    thumbnail           TEXT,
    error               TEXT,
    start_download_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    downloaded_at       TIMESTAMPTZ,
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_videos_broadcaster_id ON videos (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_videos_stream_id ON videos (stream_id);
CREATE INDEX IF NOT EXISTS idx_videos_status ON videos (status);
CREATE INDEX IF NOT EXISTS idx_videos_downloaded_at ON videos (downloaded_at DESC);
