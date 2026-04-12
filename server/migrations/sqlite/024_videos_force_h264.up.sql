-- See postgres/024_videos_force_h264.up.sql for design comments.
-- SQLite has no native BOOLEAN; stored as INTEGER (0/1) matching the
-- DownloadSchedule conventions elsewhere in this schema. Default 0
-- keeps existing rows on the HEVC-preferred path.
ALTER TABLE videos ADD COLUMN force_h264 INTEGER NOT NULL DEFAULT 0;
