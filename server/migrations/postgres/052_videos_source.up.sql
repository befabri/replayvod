-- See sqlite/052_videos_source.up.sql.
ALTER TABLE videos ADD COLUMN source TEXT NOT NULL DEFAULT 'live' CHECK (source IN ('live', 'vod'));
ALTER TABLE videos ADD COLUMN twitch_video_id TEXT;
ALTER TABLE videos ADD COLUMN broadcast_at TIMESTAMPTZ;
ALTER TABLE videos ADD COLUMN next_retry_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN attempt INTEGER NOT NULL DEFAULT 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_videos_open_twitch_video_id
    ON videos (twitch_video_id)
    WHERE twitch_video_id IS NOT NULL AND deleted_at IS NULL
      AND (status <> 'FAILED' OR next_retry_at IS NOT NULL);
CREATE INDEX IF NOT EXISTS idx_videos_source_status ON videos (source, status);
CREATE INDEX IF NOT EXISTS idx_videos_archive_retry ON videos (next_retry_at) WHERE next_retry_at IS NOT NULL;
