-- SQLite ALTER TABLE DROP COLUMN works in modern builds; no rebuild
-- needed since the CHECK constraint lives on the column itself.
ALTER TABLE videos DROP COLUMN completion_kind;
