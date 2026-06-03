-- Twitch channel view counts can exceed int32; SQLite already stores int64.
ALTER TABLE channels
    ALTER COLUMN view_count TYPE BIGINT;
