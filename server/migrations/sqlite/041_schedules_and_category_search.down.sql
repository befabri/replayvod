-- Reverse order of the up migration.
ALTER TABLE server_settings DROP COLUMN schedules_paused;

DROP INDEX IF EXISTS idx_video_categories_category_id_video_id;

DROP TABLE IF EXISTS category_search_cache;

ALTER TABLE download_schedules DROP COLUMN force_h264;
ALTER TABLE download_schedules DROP COLUMN recording_type;
