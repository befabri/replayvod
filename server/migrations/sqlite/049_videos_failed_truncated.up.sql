-- See postgres/049_videos_failed_truncated.up.sql.
UPDATE videos SET truncated = 0
WHERE status = 'FAILED'
  AND deleted_at IS NULL
  AND coalesce(videos.size_bytes, 0) <= 0
  AND NOT EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id AND vp.size_bytes > 0);
