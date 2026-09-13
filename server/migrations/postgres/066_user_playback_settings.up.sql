-- Playback preferences belong to the viewer, independent of recording state.
ALTER TABLE settings ADD COLUMN resume_min_seconds BIGINT NOT NULL DEFAULT 5 CHECK (resume_min_seconds BETWEEN 1 AND 600);
ALTER TABLE settings ADD COLUMN resume_end_margin_seconds BIGINT NOT NULL DEFAULT 30 CHECK (resume_end_margin_seconds BETWEEN 1 AND 600);
ALTER TABLE settings ADD COLUMN resume_end_margin_percent BIGINT NOT NULL DEFAULT 5 CHECK (resume_end_margin_percent BETWEEN 1 AND 50);
