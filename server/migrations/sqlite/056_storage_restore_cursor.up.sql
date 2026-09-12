-- NULL means no restore pass is pending; zero starts a pass, and positive
-- values resume after the last completed page, independently of normal scans.
ALTER TABLE server_settings ADD COLUMN storage_restore_cursor INTEGER CHECK (storage_restore_cursor >= 0);
