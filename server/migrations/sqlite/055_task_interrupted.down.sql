CREATE TABLE tasks_new (
    name                TEXT PRIMARY KEY,
    description         TEXT NOT NULL DEFAULT '',
    interval_seconds    INTEGER NOT NULL DEFAULT 0
                        CHECK (interval_seconds >= 0),
    is_enabled          INTEGER NOT NULL DEFAULT 1,
    last_run_at         TEXT,
    last_duration_ms    INTEGER NOT NULL DEFAULT 0,
    last_status         TEXT NOT NULL DEFAULT 'pending'
                        CHECK (last_status IN ('pending', 'running', 'success', 'failed', 'skipped')),
    last_error          TEXT,
    next_run_at         TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO tasks_new (name, description, interval_seconds, is_enabled, last_run_at, last_duration_ms, last_status, last_error, next_run_at, created_at, updated_at) SELECT name, description, interval_seconds, is_enabled, last_run_at, last_duration_ms, CASE WHEN last_status = 'interrupted' THEN 'skipped' ELSE last_status END, last_error, next_run_at, created_at, updated_at FROM tasks;
DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;
CREATE INDEX IF NOT EXISTS idx_tasks_next_run_at ON tasks (next_run_at) WHERE is_enabled = 1;
CREATE INDEX IF NOT EXISTS idx_tasks_last_status ON tasks (last_status);
