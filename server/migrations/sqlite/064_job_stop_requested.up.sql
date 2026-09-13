ALTER TABLE jobs ADD COLUMN stop_requested INTEGER NOT NULL DEFAULT 0 CHECK (stop_requested IN (0, 1));
