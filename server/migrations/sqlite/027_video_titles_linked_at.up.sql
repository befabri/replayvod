-- See postgres/027_video_titles_linked_at.up.sql for design.
-- SQLite's ALTER TABLE ADD COLUMN forbids non-constant defaults
-- (including CURRENT_TIMESTAMP), so we follow the table-rebuild
-- pattern established by migration 022. Composite-PK only, no
-- secondary indexes on video_titles to restore.
PRAGMA foreign_keys=OFF;

CREATE TABLE video_titles_new (
    video_id   INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    title_id   INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    linked_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (video_id, title_id)
);

INSERT INTO video_titles_new (video_id, title_id, linked_at)
SELECT video_id, title_id, CURRENT_TIMESTAMP FROM video_titles;

DROP TABLE video_titles;
ALTER TABLE video_titles_new RENAME TO video_titles;

PRAGMA foreign_keys=ON;
