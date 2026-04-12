-- Relax video_parts.codec CHECK to include 'aac' for audio-only
-- recordings. In audio mode the part carries the Twitch `audio_only`
-- HLS rendition (AAC in MP4 container); video codec enums don't
-- apply. segment_format still carries 'ts' or 'fmp4' unchanged —
-- audio_only is delivered in whichever container the channel uses
-- for video.
ALTER TABLE video_parts DROP CONSTRAINT IF EXISTS video_parts_codec_check;
ALTER TABLE video_parts ADD CONSTRAINT video_parts_codec_check
    CHECK (codec IN ('h264', 'h265', 'av1', 'aac'));
