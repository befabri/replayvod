ALTER TABLE video_parts DROP COLUMN IF EXISTS updated_at;
ALTER TABLE video_parts DROP CONSTRAINT IF EXISTS video_parts_filename_unique;
-- SET NOT NULL fails if any row has NULL end_media_seq. Callers
-- rolling this back must first UPDATE ... SET end_media_seq =
-- start_media_seq WHERE end_media_seq IS NULL (or drop those rows).
ALTER TABLE video_parts ALTER COLUMN end_media_seq SET NOT NULL;
