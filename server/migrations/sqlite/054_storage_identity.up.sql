-- storage_id is the identity the data volume must carry (the .replayvod-storage
-- marker) before the server records into it, scans it, or tombstones from it.
-- Empty means no storage has been attached yet. storage_scan_cursor is where a
-- deadline-bounded storage scan resumes, so a restart does not start over.
ALTER TABLE server_settings ADD COLUMN storage_id          TEXT    NOT NULL DEFAULT '';
ALTER TABLE server_settings ADD COLUMN storage_scan_cursor INTEGER NOT NULL DEFAULT 0;
