-- These intents survive deletion of application rows. In particular, an
-- unresolved remote upload can finish after an earlier delete observed absence.
CREATE TABLE media_publications (
    key TEXT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    digest TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    unresolved BOOLEAN NOT NULL DEFAULT FALSE,
    delete_requested BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_media_publications_recording ON media_publications (video_id, key);
