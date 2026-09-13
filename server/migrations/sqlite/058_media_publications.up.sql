-- These intents survive deletion of application rows. In particular, an
-- unresolved remote upload can finish after an earlier delete observed absence.
CREATE TABLE media_publications (
    key TEXT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    digest TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    unresolved INTEGER NOT NULL DEFAULT 0,
    delete_requested INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_media_publications_recording ON media_publications (video_id, key);
