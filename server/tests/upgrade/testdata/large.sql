WITH RECURSIVE seq(n) AS (SELECT 3 UNION ALL SELECT n+1 FROM seq WHERE n < 5002)
INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, status)
 SELECT n, 'bulk-job-' || n, 'bulk-file-' || n, 'Bulk recording ' || n, '2001', 'DONE' FROM seq;
INSERT INTO video_requests (video_id, user_id, requested_at)
 SELECT id, '1003', '2025-02-03 04:05:06' FROM videos WHERE id >= 3;
