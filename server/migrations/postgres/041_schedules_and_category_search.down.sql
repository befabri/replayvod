-- Reverse order of the up migration.
ALTER TABLE server_settings DROP COLUMN IF EXISTS schedules_paused;

DROP INDEX IF EXISTS idx_video_categories_category_id_video_id;

DROP TABLE IF EXISTS category_search_cache;

ALTER TABLE download_schedules DROP COLUMN IF EXISTS force_h264;
ALTER TABLE download_schedules DROP COLUMN IF EXISTS recording_type;
