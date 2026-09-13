-- name: GetVideoWaveformKey :one
SELECT key FROM video_waveform_assets WHERE video_id = ?1;

-- name: SetVideoWaveformKey :exec
INSERT INTO video_waveform_assets(video_id, key) VALUES (?1, ?2)
ON CONFLICT(video_id) DO UPDATE SET key = EXCLUDED.key;

-- name: DeleteVideoWaveformKey :exec
DELETE FROM video_waveform_assets WHERE video_id = ?1;
