-- Older releases only know retention and manual, and a missing-media tombstone
-- is closest to an operator removal.
UPDATE videos SET deletion_kind = 'manual' WHERE deletion_kind = 'missing';
ALTER TABLE videos DROP CONSTRAINT videos_deletion_kind_check;
ALTER TABLE videos ADD CONSTRAINT videos_deletion_kind_check
    CHECK (deletion_kind IS NULL OR deletion_kind IN ('retention', 'manual'));
