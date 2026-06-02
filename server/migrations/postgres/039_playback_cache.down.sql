ALTER TABLE server_settings DROP COLUMN IF EXISTS playback_cache_auto_generate;
ALTER TABLE server_settings DROP COLUMN IF EXISTS playback_cache_max_percent;
ALTER TABLE server_settings DROP COLUMN IF EXISTS playback_cache_enabled;

DROP INDEX IF EXISTS idx_video_playback_assets_lru;
DROP INDEX IF EXISTS idx_video_playback_assets_filename;
DROP TABLE IF EXISTS video_playback_assets;

ALTER TABLE video_metadata_changes DROP COLUMN IF EXISTS media_offset_seconds;
