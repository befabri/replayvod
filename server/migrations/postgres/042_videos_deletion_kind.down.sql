DROP INDEX IF EXISTS idx_videos_delete_requested_at;
ALTER TABLE videos DROP COLUMN IF EXISTS delete_requested_at;
ALTER TABLE videos DROP COLUMN IF EXISTS deletion_kind;
