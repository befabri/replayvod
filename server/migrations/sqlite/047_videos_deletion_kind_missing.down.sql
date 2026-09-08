UPDATE videos SET deletion_kind = 'manual' WHERE deletion_kind = 'missing';
ALTER TABLE videos ADD COLUMN deletion_kind_prev TEXT;
UPDATE videos SET deletion_kind_prev = deletion_kind;
ALTER TABLE videos DROP COLUMN deletion_kind;
ALTER TABLE videos ADD COLUMN deletion_kind TEXT
    CHECK (deletion_kind IS NULL OR deletion_kind IN ('retention', 'manual'));
UPDATE videos SET deletion_kind = deletion_kind_prev;
ALTER TABLE videos DROP COLUMN deletion_kind_prev;
