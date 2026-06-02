-- Playback cache: concatenated single-file playback artifacts for multi-part
-- recordings, the server settings that gate them, and the metadata-change media
-- offset used to map title/category changes onto the concatenated timeline.

ALTER TABLE video_metadata_changes ADD COLUMN IF NOT EXISTS media_offset_seconds DOUBLE PRECISION;

CREATE TABLE IF NOT EXISTS video_playback_assets (
    video_id            BIGINT PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
    status              TEXT NOT NULL
                        CHECK (status IN ('building', 'ready', 'failed', 'unavailable')),
    filename            TEXT,
    mime_type           TEXT,
    duration_seconds    DOUBLE PRECISION,
    size_bytes          BIGINT,
    error               TEXT,
    generated_at        TIMESTAMPTZ,
    last_accessed_at    TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (
        (
            status = 'ready'
            AND filename IS NOT NULL
            AND mime_type IS NOT NULL
            AND duration_seconds IS NOT NULL
            AND size_bytes IS NOT NULL
            AND generated_at IS NOT NULL
            AND last_accessed_at IS NOT NULL
        )
        OR (
            status IN ('building', 'failed', 'unavailable')
            AND filename IS NULL
            AND mime_type IS NULL
            AND last_accessed_at IS NULL
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_video_playback_assets_filename
ON video_playback_assets (filename)
WHERE filename IS NOT NULL;

-- Serves ListReadyVideoPlaybackAssets (the LRU eviction scan), which runs on
-- every build and every reconcile tick: WHERE status = 'ready' ORDER BY
-- last_accessed_at, generated_at, video_id.
CREATE INDEX IF NOT EXISTS idx_video_playback_assets_lru
ON video_playback_assets (last_accessed_at, generated_at, video_id)
WHERE status = 'ready';

ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS playback_cache_enabled       BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS playback_cache_max_percent   INTEGER NOT NULL DEFAULT 10;
ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS playback_cache_auto_generate BOOLEAN NOT NULL DEFAULT FALSE;
