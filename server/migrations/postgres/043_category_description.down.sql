ALTER TABLE categories
    DROP COLUMN IF EXISTS game_metadata_checked_at,
    DROP COLUMN IF EXISTS description_checked_at,
    DROP COLUMN IF EXISTS description;
