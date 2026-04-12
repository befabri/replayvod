-- recording_type distinguishes video-mode jobs (mp4 with video + audio
-- tracks) from audio-mode jobs (m4a, audio_only HLS rendition). Set at
-- job creation; immutable for the life of the row. Downstream code
-- branches on this for Stage 3 variant selection, Stage 6 output
-- extension, Stage 8 thumbnail (skipped for audio), and Stage 9
-- corruption check (audio stream instead of video stream).
--
-- DEFAULT 'video' backfills existing rows and keeps legacy callers
-- that don't set this field working unchanged.
ALTER TABLE videos ADD COLUMN recording_type TEXT NOT NULL DEFAULT 'video'
    CHECK (recording_type IN ('video', 'audio'));
