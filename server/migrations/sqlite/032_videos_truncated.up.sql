-- See migrations/postgres/032_videos_truncated.up.sql for rationale.
-- SQLite uses INTEGER 0/1 for booleans; sqlc maps the column to int64
-- in generated Go and the adapter flattens to bool at the boundary.
ALTER TABLE videos ADD COLUMN truncated INTEGER NOT NULL DEFAULT 0;
UPDATE videos SET truncated = 1 WHERE completion_kind IN ('partial', 'cancelled');
