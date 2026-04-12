-- force_h264 is a per-job codec preference: when TRUE, Stage 3 drops
-- HEVC and AV1 variants before running the quality fallback chain, so
-- the recording always uses H.264 regardless of what Twitch's codec
-- ladder offers. Ignored when recording_type='audio'. Default FALSE
-- keeps HEVC-preferred behavior for existing and new jobs.
--
-- See .docs/spec/download-pipeline.md section "User codec preference
-- (force_h264)" and AC-FORCE-1..4.
ALTER TABLE videos ADD COLUMN force_h264 BOOLEAN NOT NULL DEFAULT FALSE;
