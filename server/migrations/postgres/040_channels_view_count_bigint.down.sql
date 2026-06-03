-- This truncates values above int32 and is unsafe after large counts exist.
ALTER TABLE channels
    ALTER COLUMN view_count TYPE INTEGER;
