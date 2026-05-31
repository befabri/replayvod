-- Retention is a recording policy snapshot, not a live broadcaster setting.
-- Manual recordings and schedule recordings with no matching delete policy keep
-- retention_window_hours NULL and are never selected by the sweep.
ALTER TABLE videos ADD COLUMN trigger_schedule_id INTEGER REFERENCES download_schedules(id) ON DELETE SET NULL;
ALTER TABLE videos ADD COLUMN retention_source_schedule_id INTEGER REFERENCES download_schedules(id) ON DELETE SET NULL;
ALTER TABLE videos ADD COLUMN retention_window_hours INTEGER
    -- 2562047 is floor(MaxInt64 / time.Hour), so Go duration conversion cannot overflow.
    CHECK (
        retention_window_hours IS NULL
        OR (retention_window_hours > 0 AND retention_window_hours <= 2562047)
    );

CREATE INDEX IF NOT EXISTS idx_videos_retention_due
    ON videos (downloaded_at)
    WHERE deleted_at IS NULL AND retention_window_hours IS NOT NULL;
