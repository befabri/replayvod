-- See postgres/047_recording_quality.down.sql. The columns are rebuilt because
-- SQLite cannot alter a CHECK in place.
ALTER TABLE videos RENAME COLUMN quality TO quality_legacy;
ALTER TABLE videos ADD COLUMN quality TEXT NOT NULL DEFAULT 'HIGH' CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH'));
UPDATE videos SET quality = CASE WHEN quality_legacy IN ('1440', 'BEST') THEN 'HIGH' ELSE quality_legacy END;
ALTER TABLE videos DROP COLUMN quality_legacy;
ALTER TABLE download_schedules RENAME COLUMN quality TO quality_legacy;
ALTER TABLE download_schedules ADD COLUMN quality TEXT NOT NULL DEFAULT 'HIGH' CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH'));
UPDATE download_schedules SET quality = CASE WHEN quality_legacy IN ('1440', 'BEST') THEN 'HIGH' ELSE quality_legacy END;
ALTER TABLE download_schedules DROP COLUMN quality_legacy;
