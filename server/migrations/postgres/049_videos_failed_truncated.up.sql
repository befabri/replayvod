-- Empty failed runs were marked truncated when their playlist never closed.
-- Legacy single-file recordings may own bytes before they have part rows.
UPDATE videos SET truncated = FALSE
WHERE status = 'FAILED'
  AND deleted_at IS NULL
  AND coalesce(videos.size_bytes, 0) <= 0
  AND NOT EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id AND vp.size_bytes > 0);
