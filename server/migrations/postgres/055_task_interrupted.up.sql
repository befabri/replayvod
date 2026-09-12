-- Interrupted work is eligible immediately; shutdown is not a task failure.
ALTER TABLE tasks DROP CONSTRAINT tasks_last_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_last_status_check CHECK (last_status IN ('pending', 'running', 'success', 'failed', 'skipped', 'interrupted'));
