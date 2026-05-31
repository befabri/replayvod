DROP INDEX IF EXISTS idx_videos_retention_due;

ALTER TABLE videos DROP COLUMN retention_window_hours;
ALTER TABLE videos DROP COLUMN retention_source_schedule_id;
ALTER TABLE videos DROP COLUMN trigger_schedule_id;
