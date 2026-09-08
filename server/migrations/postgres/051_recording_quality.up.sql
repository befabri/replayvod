-- '1440' caps a recording at 1440p; 'BEST' takes the highest rendition Twitch offers.
ALTER TABLE videos DROP CONSTRAINT videos_quality_check;
ALTER TABLE videos ADD CONSTRAINT videos_quality_check CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH', '1440', 'BEST'));
ALTER TABLE download_schedules DROP CONSTRAINT download_schedules_quality_check;
ALTER TABLE download_schedules ADD CONSTRAINT download_schedules_quality_check CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH', '1440', 'BEST'));
