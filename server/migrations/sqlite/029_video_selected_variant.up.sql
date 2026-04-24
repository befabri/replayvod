ALTER TABLE videos ADD COLUMN selected_quality TEXT;
ALTER TABLE videos ADD COLUMN selected_fps REAL;

ALTER TABLE video_parts ADD COLUMN fps REAL;

UPDATE videos
SET selected_quality = (
        SELECT quality
        FROM video_parts
        WHERE video_parts.video_id = videos.id
        ORDER BY part_index DESC
        LIMIT 1
    ),
    selected_fps = (
        SELECT fps
        FROM video_parts
        WHERE video_parts.video_id = videos.id
        ORDER BY part_index DESC
        LIMIT 1
    )
WHERE EXISTS (
    SELECT 1
    FROM video_parts
    WHERE video_parts.video_id = videos.id
);
