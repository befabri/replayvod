INSERT INTO channels (broadcaster_id, broadcaster_login, broadcaster_name) VALUES
 ('2003', 'resume_channel', 'Resume channel'),
 ('2004', 'partial_channel', 'Partial channel'),
 ('2005', 'empty_channel', 'Empty channel');
INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, status) VALUES
 (6001, 'upgrade-resume', 'upgrade-resume', 'Interrupted remux', '2003', 'RUNNING'),
 (6002, 'upgrade-partial', 'upgrade-partial', 'Damaged checkpoint with saved part', '2004', 'RUNNING'),
 (6003, 'upgrade-empty', 'upgrade-empty', 'Damaged checkpoint without saved part', '2005', 'RUNNING');
INSERT INTO jobs (id, video_id, broadcaster_id, status, started_at, resume_state) VALUES
 ('upgrade-resume', 6001, '2003', 'RUNNING', '2025-02-03 04:05:06', '{"stage":"PREPARE_INPUT","current_part_index":1,"selected_quality":"HIGH","selected_codec":"h264","segment_format":"ts","part_start_media_sequence":0,"part_started":true,"accounted_frontier_media_sequence":0,"endlist_seen":true}'),
 ('upgrade-partial', 6002, '2004', 'RUNNING', '2025-02-03 04:05:06', '{"stage":42}'),
 ('upgrade-empty', 6003, '2005', 'RUNNING', '2025-02-03 04:05:06', '{"stage":42}');
INSERT INTO video_parts (video_id, part_index, filename, quality, codec, segment_format, start_media_seq, end_media_seq, duration_seconds, size_bytes)
 VALUES (6002, 1, 'upgrade-partial-part01.mp4', 'HIGH', 'h264', 'fmp4', 0, 0, 1, 1552);
