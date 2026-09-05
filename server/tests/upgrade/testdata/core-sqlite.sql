INSERT INTO users (id, login, display_name, role) VALUES
 ('1001', 'upgrade_owner', 'Owner', 'owner'),
 ('1002', 'upgrade_admin', 'Admin', 'admin'),
 ('1003', 'upgrade_viewer', 'Viewer é日本語', 'viewer');
INSERT INTO whitelist (twitch_user_id) VALUES ('1001'), ('1002'), ('1003'), ('1004');
INSERT INTO channels (broadcaster_id, broadcaster_login, broadcaster_name) VALUES
 ('2001', 'old_channel', 'Existing channel'), ('2002', 'new_channel', 'Request channel');
INSERT INTO categories (id, name) VALUES ('3001', 'Games é日本語');
INSERT INTO tags (id, name) VALUES (1, 'English');
INSERT INTO titles (id, name) VALUES (1, 'A viewer''s recording');
INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, status, duration_seconds, size_bytes) VALUES
 (1, 'upgrade-recording', 'upgrade-recording', 'Preserved recording', '2001', 'DONE', 180, 2048),
 (2, 'upgrade-failed', 'upgrade-failed', 'Failed recording', '2001', 'FAILED', NULL, NULL);
INSERT INTO video_parts (id, video_id, part_index, filename, quality, codec, segment_format, start_media_seq, end_media_seq, duration_seconds, size_bytes)
 VALUES (1, 1, 0, 'upgrade-recording-part00.mp4', 'HIGH', 'h264', 'fmp4', 0, 1, 180, 2048);
INSERT INTO video_titles (video_id, title_id) VALUES (1, 1);
INSERT INTO video_categories (video_id, category_id) VALUES (1, '3001');
INSERT INTO video_tags (video_id, tag_id) VALUES (1, 1);
INSERT INTO video_requests (video_id, user_id, requested_at) VALUES
 (1, '1003', '2025-02-03 04:05:06'), (2, '1003', '2025-02-03 04:05:06'), (1, '1002', '2025-02-03 04:05:06');
INSERT INTO download_schedules (id, broadcaster_id, requested_by, quality, has_min_viewers, min_viewers,
 has_categories, has_tags, is_delete_rediff, time_before_delete, last_triggered_at, trigger_count, recording_type, force_h264)
 VALUES (1, '2001', '1002', 'MEDIUM', TRUE, 250, TRUE, TRUE, TRUE, 48, '2025-02-03 04:05:06', 17, 'video', FALSE);
INSERT INTO download_schedules (id, broadcaster_id, requested_by, quality, is_disabled, recording_type, force_h264)
 VALUES (2, '2001', '1003', 'LOW', TRUE, 'audio', TRUE);
INSERT INTO download_schedule_categories (schedule_id, category_id) VALUES (1, '3001');
INSERT INTO download_schedule_tags (schedule_id, tag_id) VALUES (1, 1);
UPDATE server_settings SET server_mode = 'off', hmac_secret = 'upgrade-preserved-hmac',
 playback_cache_enabled = FALSE, playback_cache_max_percent = 25, schedules_paused = TRUE;
INSERT INTO sessions (hashed_id, user_id, encrypted_tokens, expires_at) VALUES ('ffe054fe7ae0cb6dc65c3af9b61d5209f439851db43d0ba5997337df154668eb', '1001', X'0101010101010101010101013bea6710bd67cb034896351dfb094a77e6f318fb02a753ea79589c2879b4c4cf0d6a94bb9e2959b540d4f34aa36f56493e46ec3be325ca2470ce665155d34c54656cfe69c89235b3d456a9b7e0ca9c7ce45f09f63aac82be5411dc770415367fc96a402c43d46de5325dbf71f2173a1ba3ad66af8779caedf62b88', '2099-01-01 00:00:00');
INSERT INTO sessions (hashed_id, user_id, encrypted_tokens, expires_at) VALUES ('a0fab1377f49a759b57f63318262ebe89fabfc990e8e93ceac2984561482b9d4', '1002', X'0202020202020202020202021fe63541cef9ddd371fcdcd0e63cd0bf7bcfa7a2db00254c9c4f1e0d9005594a88f9093ca99e6d97e30f80dd7a822ea9f3e9373fe99d9a4ec62a783f6a612ee30eec816847873964b2e93d00ee8968a360c6847691af43da04fc539db743dff23f389ebf7b7e6016397aeb17aa53fda8b426f7d1f0cc680112ba81', '2099-01-01 00:00:00');
INSERT INTO sessions (hashed_id, user_id, encrypted_tokens, expires_at) VALUES ('52b6419d27bd7f547cee3b92f8c17a908b8a49601ecbec161e5030de1dfe9e0a', '1003', X'0303030303030303030303038b5497a212ea806da55e7ea2748312d76f33c6a93230a38314fb3af1cfa40532da3b37583c6fa1071c9038d0122dc60e8656546e2c592526b64e744d2306558fcead196b29ba7d8df8e56f4ef9fe0eafdb6ebef94f26483c098eaf254ac266179ba71b68690e4cd4a33e08ab5b4b5e7b57e69f1a216543b1ad2145', '2099-01-01 00:00:00');
