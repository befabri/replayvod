-- Failed runs that never finalized a part were stamped truncated because the
-- playlist never closed; they captured nothing, so they failed rather than
-- stopped early. Clear the flag on live rows, matching the recorder's rule.
UPDATE videos SET truncated = FALSE
WHERE status = 'FAILED'
  AND deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM video_parts vp WHERE vp.video_id = videos.id AND vp.size_bytes > 0);
