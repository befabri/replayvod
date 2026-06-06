ALTER TABLE categories ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE categories ADD COLUMN IF NOT EXISTS description_checked_at TIMESTAMPTZ;
ALTER TABLE categories ADD COLUMN IF NOT EXISTS game_metadata_checked_at TIMESTAMPTZ;
