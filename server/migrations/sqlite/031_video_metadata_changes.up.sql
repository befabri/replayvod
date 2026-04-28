-- Append-only event log of channel.update observations attached to a
-- recording. See the postgres copy of this migration for the full
-- rationale; SQLite mirrors the same shape with INTEGER/TEXT and the
-- adapter's "2006-01-02 15:04:05" text-time format.
CREATE TABLE video_metadata_changes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id     INTEGER NOT NULL REFERENCES videos(id)     ON DELETE CASCADE,
    occurred_at  TEXT    NOT NULL,
    title_id     INTEGER          REFERENCES titles(id)     ON DELETE RESTRICT,
    category_id  TEXT             REFERENCES categories(id) ON DELETE RESTRICT,
    CHECK (title_id IS NOT NULL OR category_id IS NOT NULL)
);

CREATE INDEX idx_video_metadata_changes_video_id_occurred_at
    ON video_metadata_changes (video_id, occurred_at, id);
