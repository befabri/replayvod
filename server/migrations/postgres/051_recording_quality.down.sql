-- Older releases only know LOW, MEDIUM and HIGH, so the wider choices fold back into HIGH.
UPDATE videos SET quality = 'HIGH' WHERE quality IN ('1440', 'BEST');
ALTER TABLE videos DROP CONSTRAINT videos_quality_check;
ALTER TABLE videos ADD CONSTRAINT videos_quality_check CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH'));
UPDATE download_schedules SET quality = 'HIGH' WHERE quality IN ('1440', 'BEST');
ALTER TABLE download_schedules DROP CONSTRAINT download_schedules_quality_check;
ALTER TABLE download_schedules ADD CONSTRAINT download_schedules_quality_check CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH'));
