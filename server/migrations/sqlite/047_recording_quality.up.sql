-- See postgres/047_recording_quality.up.sql. SQLite cannot alter a CHECK in
-- place, so each column is rebuilt and its values copied across.
ALTER TABLE videos RENAME COLUMN quality TO quality_legacy;
ALTER TABLE videos ADD COLUMN quality TEXT NOT NULL DEFAULT 'HIGH' CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH', '1440', 'BEST'));
UPDATE videos SET quality = quality_legacy;
ALTER TABLE videos DROP COLUMN quality_legacy;
ALTER TABLE download_schedules RENAME COLUMN quality TO quality_legacy;
ALTER TABLE download_schedules ADD COLUMN quality TEXT NOT NULL DEFAULT 'HIGH' CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH', '1440', 'BEST'));
UPDATE download_schedules SET quality = quality_legacy;
ALTER TABLE download_schedules DROP COLUMN quality_legacy;
