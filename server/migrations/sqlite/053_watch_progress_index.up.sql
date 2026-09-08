-- A revision distinguishes accepted writes even within one clock millisecond.
ALTER TABLE video_user_states ADD COLUMN progress_revision BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_video_user_states_progress ON video_user_states (user_id, last_progress_at_ms DESC, video_id DESC);
