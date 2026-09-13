CREATE TABLE video_waveform_assets (
    video_id BIGINT PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
    key TEXT NOT NULL UNIQUE
);

-- Existing waveforms keep their stored location. A missing artifact is rebuilt
-- lazily at a new immutable key; readers never infer an old filename.
INSERT INTO video_waveform_assets(video_id, key)
SELECT id, 'thumbnails/' || filename || '-waveform.json'
FROM videos WHERE recording_type = 'audio';

INSERT INTO media_publications(key, video_id, digest, size_bytes)
SELECT key, video_id, '', 0 FROM video_waveform_assets
ON CONFLICT(key) DO NOTHING;
