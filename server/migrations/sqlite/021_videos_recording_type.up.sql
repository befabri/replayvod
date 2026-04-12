-- See postgres/021_videos_recording_type.up.sql for design comments.
-- SQLite 3.25+ allows ADD COLUMN with CHECK. The constraint is
-- attached to the column, not the table, so this works without a
-- full table rebuild.
ALTER TABLE videos ADD COLUMN recording_type TEXT NOT NULL DEFAULT 'video'
    CHECK (recording_type IN ('video', 'audio'));
