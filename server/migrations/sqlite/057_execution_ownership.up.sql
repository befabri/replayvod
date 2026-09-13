ALTER TABLE jobs ADD COLUMN execution_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN accepts_metadata INTEGER NOT NULL DEFAULT 0 CHECK (accepts_metadata IN (0, 1));
ALTER TABLE tasks ADD COLUMN execution_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_jobs_recovery ON jobs (id) WHERE status IN ('PENDING', 'RUNNING');

UPDATE videos SET status = 'FAILED',
    error = 'Admission interrupted before an attempt was created',
    completion_kind = CASE WHEN EXISTS (
        SELECT 1 FROM video_parts WHERE video_id = videos.id AND size_bytes > 0
    ) THEN 'partial' ELSE 'complete' END
WHERE status IN ('PENDING', 'RUNNING')
  AND NOT EXISTS (SELECT 1 FROM jobs WHERE jobs.id = videos.job_id);
