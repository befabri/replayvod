UPDATE tasks SET last_status = 'skipped' WHERE last_status = 'interrupted';
ALTER TABLE tasks DROP CONSTRAINT tasks_last_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_last_status_check CHECK (last_status IN ('pending', 'running', 'success', 'failed', 'skipped'));
