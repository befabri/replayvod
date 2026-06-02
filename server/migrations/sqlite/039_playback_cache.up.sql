-- Playback cache: concatenated single-file playback artifacts for multi-part
-- recordings, the server settings that gate them, and the metadata-change media
-- offset used to map title/category changes onto the concatenated timeline.

ALTER TABLE video_metadata_changes ADD COLUMN media_offset_seconds REAL;

CREATE TABLE IF NOT EXISTS video_playback_assets (
    video_id            INTEGER PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
    status              TEXT NOT NULL
                        CHECK (status IN ('building', 'ready', 'failed', 'unavailable')),
    filename            TEXT,
    mime_type           TEXT,
    duration_seconds    REAL,
    size_bytes          INTEGER,
    error               TEXT,
    generated_at        TEXT,
    last_accessed_at    TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),

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

ALTER TABLE server_settings ADD COLUMN playback_cache_enabled       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE server_settings ADD COLUMN playback_cache_max_percent   INTEGER NOT NULL DEFAULT 10;
ALTER TABLE server_settings ADD COLUMN playback_cache_auto_generate INTEGER NOT NULL DEFAULT 0;
