ALTER TABLE videos DROP CONSTRAINT IF EXISTS videos_completion_kind_chk;
ALTER TABLE videos DROP COLUMN completion_kind;
