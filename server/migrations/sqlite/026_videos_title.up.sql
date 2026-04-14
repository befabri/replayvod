-- See postgres/026_videos_title.up.sql for design comments. SQLite
-- accepts the same ALTER TABLE ADD COLUMN shape as Postgres; no
-- table-rebuild ritual is needed because this change only adds a
-- column with a constant default.
ALTER TABLE videos ADD COLUMN title TEXT NOT NULL DEFAULT '';
