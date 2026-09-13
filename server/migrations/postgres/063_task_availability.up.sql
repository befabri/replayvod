-- Registration reflects this process's configuration. It must never replace
-- the operator's durable pause state or erase task history.
ALTER TABLE tasks ADD COLUMN is_available BOOLEAN NOT NULL DEFAULT FALSE;
