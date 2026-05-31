DROP INDEX IF EXISTS idx_videos_retention_due;

ALTER TABLE videos
    DROP CONSTRAINT IF EXISTS chk_videos_retention_window_hours,
    DROP COLUMN IF EXISTS retention_window_hours,
    DROP COLUMN IF EXISTS retention_source_schedule_id,
    DROP COLUMN IF EXISTS trigger_schedule_id;
