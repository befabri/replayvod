-- Supplemental historical data for 048/049. Keep the original release fixtures
-- frozen. These rows use only columns present in every retained baseline.
UPDATE videos SET thumbnail = 'retained-live-poster.jpg' WHERE id = 1;
INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, status, thumbnail, truncated, deleted_at, deletion_kind) VALUES
 (8001, 'old-manual', 'old-manual', 'Manual tombstone', '2001', 'DONE', 'purged-manual.jpg', FALSE, '2025-03-01 00:00:00', 'manual'),
 (8002, 'old-retention', 'old-retention', 'Retention tombstone', '2001', 'DONE', 'purged-retention.jpg', FALSE, '2025-03-01 00:00:00', 'retention'),
 (8003, 'old-empty-failure', 'old-empty-failure', 'No saved media', '2001', 'FAILED', NULL, TRUE, NULL, NULL),
 (8004, 'old-stub-failure', 'old-stub-failure', 'Unfinished part', '2001', 'FAILED', NULL, TRUE, NULL, NULL),
 (8005, 'old-saved-failure', 'old-saved-failure', 'Saved part', '2001', 'FAILED', NULL, TRUE, NULL, NULL),
 (8006, 'old-deleted-failure', 'old-deleted-failure', 'Deleted failure', '2001', 'FAILED', NULL, TRUE, '2025-03-01 00:00:00', 'retention'),
 (8007, 'old-complete', 'old-complete', 'Completed recording', '2001', 'DONE', NULL, TRUE, NULL, NULL);
INSERT INTO video_parts (id, video_id, part_index, filename, quality, codec, segment_format, start_media_seq, end_media_seq, size_bytes) VALUES
 (9001, 8004, 0, 'old-unfinished-part.mp4', 'HIGH', 'h264', 'fmp4', 0, NULL, 0),
 (9002, 8005, 0, 'old-saved-part.mp4', 'HIGH', 'h264', 'fmp4', 0, 1, 2048);
