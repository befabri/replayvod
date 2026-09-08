-- See postgres/047_videos_deletion_kind_missing.up.sql. SQLite cannot alter a
-- CHECK in place, so the column is rebuilt: copy the values aside, drop the
-- column (its own CHECK goes with it), add it back with the wider set, restore.
ALTER TABLE videos ADD COLUMN deletion_kind_prev TEXT;
UPDATE videos SET deletion_kind_prev = deletion_kind;
ALTER TABLE videos DROP COLUMN deletion_kind;
ALTER TABLE videos ADD COLUMN deletion_kind TEXT
    CHECK (deletion_kind IS NULL OR deletion_kind IN ('retention', 'manual', 'missing'));
UPDATE videos SET deletion_kind = deletion_kind_prev;
ALTER TABLE videos DROP COLUMN deletion_kind_prev;
-- Manual and retention tombstones purged their posters, so the stored path
-- points at nothing. Discovery keeps previews for the missing kind.
UPDATE videos SET thumbnail = NULL
WHERE deleted_at IS NOT NULL AND (deletion_kind IS NULL OR deletion_kind <> 'missing');
