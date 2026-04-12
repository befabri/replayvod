ALTER TABLE video_parts DROP CONSTRAINT IF EXISTS video_parts_codec_check;
ALTER TABLE video_parts ADD CONSTRAINT video_parts_codec_check
    CHECK (codec IN ('h264', 'h265', 'av1'));
