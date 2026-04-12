-- See postgres/017_tasks.up.sql for design comments. SQLite mirror uses
-- TEXT for timestamps and INTEGER (0/1) for booleans per the Phase 4-5
-- convention.
CREATE TABLE IF NOT EXISTS tasks (
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

CREATE INDEX IF NOT EXISTS idx_tasks_next_run_at ON tasks (next_run_at) WHERE is_enabled = 1;
CREATE INDEX IF NOT EXISTS idx_tasks_last_status ON tasks (last_status);
