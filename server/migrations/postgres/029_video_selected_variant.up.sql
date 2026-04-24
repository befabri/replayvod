ALTER TABLE videos ADD COLUMN selected_quality TEXT;
ALTER TABLE videos ADD COLUMN selected_fps DOUBLE PRECISION;

ALTER TABLE video_parts ADD COLUMN fps DOUBLE PRECISION;

UPDATE videos v
SET selected_quality = vp.quality,
    selected_fps = vp.fps
FROM (
    SELECT DISTINCT ON (video_id)
        video_id,
        quality,
        fps
    FROM video_parts
    ORDER BY video_id, part_index DESC
) vp
WHERE vp.video_id = v.id;
