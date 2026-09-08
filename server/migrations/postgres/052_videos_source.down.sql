DROP INDEX IF EXISTS idx_videos_archive_retry;
DROP INDEX IF EXISTS idx_videos_source_status;
DROP INDEX IF EXISTS idx_videos_open_twitch_video_id;
ALTER TABLE jobs DROP COLUMN attempt;
ALTER TABLE videos DROP COLUMN next_retry_at;
ALTER TABLE videos DROP COLUMN broadcast_at;
ALTER TABLE videos DROP COLUMN twitch_video_id;
ALTER TABLE videos DROP COLUMN source;
