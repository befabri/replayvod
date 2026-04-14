PRAGMA foreign_keys=OFF;

CREATE TABLE video_titles_old (
    video_id   INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    title_id   INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    PRIMARY KEY (video_id, title_id)
);

INSERT INTO video_titles_old (video_id, title_id)
SELECT video_id, title_id FROM video_titles;

DROP TABLE video_titles;
ALTER TABLE video_titles_old RENAME TO video_titles;

PRAGMA foreign_keys=ON;
