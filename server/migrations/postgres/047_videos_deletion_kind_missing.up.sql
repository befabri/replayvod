-- 'missing' tombstones a recording whose media is gone from storage (deleted
-- by hand, a moved data directory, a lost volume). The storage scan and the
-- playback 404 path set it so the library stops offering a recording that
-- cannot play.
ALTER TABLE videos DROP CONSTRAINT videos_deletion_kind_check;
ALTER TABLE videos ADD CONSTRAINT videos_deletion_kind_check
    CHECK (deletion_kind IS NULL OR deletion_kind IN ('retention', 'manual', 'missing'));
-- Manual and retention tombstones purged their posters, so the stored path
-- points at nothing. Discovery keeps previews for the missing kind.
UPDATE videos SET thumbnail = NULL
WHERE deleted_at IS NOT NULL AND (deletion_kind IS NULL OR deletion_kind <> 'missing');
