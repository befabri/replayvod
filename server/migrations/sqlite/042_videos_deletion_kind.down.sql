DROP INDEX IF EXISTS idx_videos_delete_requested_at;
ALTER TABLE videos DROP COLUMN delete_requested_at;
ALTER TABLE videos DROP COLUMN deletion_kind;
