ALTER TABLE download_schedules DROP COLUMN requested_from;

DROP TABLE IF EXISTS schedule_requests;

-- The up migration retains this table and its data. IF NOT EXISTS also lets
-- installations that ran an earlier destructive development version downgrade
-- their schema; recovering rows lost by that version still requires a backup.
CREATE TABLE IF NOT EXISTS video_requests (
    video_id     BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (video_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_video_requests_user_id ON video_requests (user_id);

DROP TABLE IF EXISTS invites;
