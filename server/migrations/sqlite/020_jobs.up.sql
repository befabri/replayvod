-- See postgres/020_jobs.up.sql for design comments. SQLite mirror uses
-- TEXT for timestamps and JSON. resume_state is stored as TEXT (default
-- '{}') rather than JSONB; the domain layer carries json.RawMessage and
-- the adapter converts.
CREATE TABLE IF NOT EXISTS jobs (
    id                  TEXT PRIMARY KEY,
    video_id            INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    broadcaster_id      TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'PENDING'
                        CHECK (status IN ('PENDING', 'RUNNING', 'DONE', 'FAILED')),
    started_at          TEXT,
    finished_at         TEXT,
    error               TEXT,
    resume_state        TEXT NOT NULL DEFAULT '{}',
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_jobs_video_id ON jobs (video_id);
CREATE INDEX IF NOT EXISTS idx_jobs_broadcaster_id ON jobs (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_jobs_active_by_broadcaster
    ON jobs (broadcaster_id)
    WHERE status IN ('PENDING', 'RUNNING');
