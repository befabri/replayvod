-- Registration reflects this process's configuration. It must never replace
-- the operator's durable pause state or erase task history.
ALTER TABLE tasks ADD COLUMN is_available INTEGER NOT NULL DEFAULT 0 CHECK (is_available IN (0, 1));
