DROP INDEX idx_jobs_recovery;
ALTER TABLE tasks DROP COLUMN execution_id;
ALTER TABLE jobs DROP COLUMN accepts_metadata;
ALTER TABLE jobs DROP COLUMN execution_id;
