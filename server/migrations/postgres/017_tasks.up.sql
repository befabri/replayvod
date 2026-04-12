-- tasks is the registry of scheduled background jobs the app runs on an
-- interval. Each row is "token_cleanup runs every 15 min, last ran at T,
-- took D ms." The scheduler ticks a single goroutine, compares NOW()
-- against next_run_at, and invokes the registered runner for that row.
--
-- Rows are seeded on startup by the scheduler itself (INSERT ... ON
-- CONFLICT DO NOTHING) so upgrading the binary with a new task
-- registers it without a migration. Operators can flip is_enabled in
-- the dashboard to pause a task without redeploying.
--
-- status + last_error carry the most recent run outcome; the history
-- lives in event_logs where each invocation writes a row.
CREATE TABLE IF NOT EXISTS tasks (
    -- Stable task key. Matches the name the scheduler code registers
    -- with — "token_cleanup", "eventsub_snapshot", etc. PK rather than
    -- a synthetic ID so code can upsert by name.
    name                TEXT PRIMARY KEY,
    description         TEXT NOT NULL DEFAULT '',

    -- Cadence in seconds. 0 disables interval firing (task only runs
    -- via the manual "run now" button). Separate from is_enabled so an
    -- operator can pause-without-losing-cadence.
    interval_seconds    INTEGER NOT NULL DEFAULT 0
                        CHECK (interval_seconds >= 0),

    is_enabled          BOOLEAN NOT NULL DEFAULT TRUE,

    -- Runtime state. last_run_at NULL means "never run"; next_run_at
    -- NULL means "no schedule" (interval_seconds=0). Updated in a
    -- single UPDATE after each invocation.
    last_run_at         TIMESTAMPTZ,
    last_duration_ms    INTEGER NOT NULL DEFAULT 0,
    last_status         TEXT NOT NULL DEFAULT 'pending'
                        CHECK (last_status IN ('pending', 'running', 'success', 'failed', 'skipped')),
    last_error          TEXT,
    next_run_at         TIMESTAMPTZ,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tasks_next_run_at ON tasks (next_run_at) WHERE is_enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_tasks_last_status ON tasks (last_status);
