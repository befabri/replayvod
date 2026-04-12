-- No-op on SQLite: full-text search is PG-only. See
-- postgres/025_event_logs_fts.up.sql for the design. The repository
-- FullTextSearcher interface is implemented only by the PG adapter;
-- the SQLite adapter does not satisfy it and services type-assert
-- to detect support.
SELECT 1;
