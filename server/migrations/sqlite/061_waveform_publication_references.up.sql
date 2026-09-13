CREATE TABLE video_waveform_assets (
    video_id INTEGER PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
    key TEXT NOT NULL UNIQUE
);

-- SQL upgrades stored references; the runtime only reads authoritative keys.
INSERT INTO video_waveform_assets(video_id, key)
SELECT id, 'thumbnails/' || filename || '-waveform.json'
FROM videos WHERE recording_type = 'audio';

INSERT INTO media_publications(key, video_id, digest, size_bytes)
SELECT key, video_id, '', 0 FROM video_waveform_assets WHERE TRUE
ON CONFLICT(key) DO NOTHING;
