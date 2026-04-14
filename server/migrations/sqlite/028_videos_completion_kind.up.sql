-- See postgres/028_videos_completion_kind.up.sql for design.
-- SQLite's ALTER TABLE ADD COLUMN accepts a literal DEFAULT but
-- CHECK constraints on the new column must either be added via
-- the table-rebuild pattern OR expressed in the ADD COLUMN clause
-- itself (sqlite3 >=3.37 supports this; we've been targeting recent
-- builds). Inline CHECK keeps this migration a single statement
-- and skips the rebuild.
ALTER TABLE videos ADD COLUMN completion_kind TEXT NOT NULL DEFAULT 'complete'
    CHECK (completion_kind IN ('complete', 'partial', 'cancelled'));
