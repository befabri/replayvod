-- jobs is the durable record of a download execution. One row per
-- attempt at turning a live stream into a stored VOD. Distinct from
-- `videos`: a video row is the logical output; a job row is the work
-- that produced (or failed to produce) it. One video can have multiple
-- jobs over time if the first FAILED and a retry succeeded — the
-- video row stays stable while jobs accumulate.
--
-- Broadcaster-level idempotency lives here: the downloader queries
-- for PENDING|RUNNING jobs on a broadcaster before starting a new one,
-- so two concurrent triggers for the same channel collapse to one
-- even after a restart that dropped the in-memory active map.
--
-- `resume_state` is a JSON blob with the schema documented in
-- .docs/spec/download-pipeline.md (section "Resume on restart"). It
-- tracks pipeline stage, accounted segment frontier, permanent gaps,
-- and per-part selection. A single JSON column is deliberate: the
-- frontier/gap accounting is accessed atomically on each segment
-- completion, and a child table would turn that into N writes per
-- segment. If the blob grows large enough to matter at very long
-- recordings, migrate gaps[] + completed_above_frontier to a child
-- `download_segments` table — see the spec for the threshold.
CREATE TABLE IF NOT EXISTS jobs (
    -- UUID string rather than a DB-native UUID type to keep the
    -- schema portable across SQLite and to match the v1 jobID
    -- convention (google/uuid.NewString).
    id                  TEXT PRIMARY KEY,
    video_id            BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    broadcaster_id      TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'PENDING'
                        CHECK (status IN ('PENDING', 'RUNNING', 'DONE', 'FAILED')),
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    error               TEXT,

    -- JSONB so postgres can index keys inside if we ever need to. Default
    -- '{}' keeps the column NOT NULL and unmarshal-safe: reading a fresh
    -- PENDING job never produces a null blob.
    resume_state        JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_jobs_video_id ON jobs (video_id);
CREATE INDEX IF NOT EXISTS idx_jobs_broadcaster_id ON jobs (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);

-- Partial index for the idempotency hot path: "is there an active job
-- for this broadcaster?" runs on every Start call. Filtering on the
-- two non-terminal states at index time keeps the lookup cheap even
-- as DONE/FAILED rows accumulate historically.
CREATE INDEX IF NOT EXISTS idx_jobs_active_by_broadcaster
    ON jobs (broadcaster_id)
    WHERE status IN ('PENDING', 'RUNNING');
