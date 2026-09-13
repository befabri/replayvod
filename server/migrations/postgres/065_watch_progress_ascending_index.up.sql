-- A backward scan of the descending index puts null timestamps first. The
-- API keeps unstarted recordings last in either direction, so ascending pages
-- need this ordering to avoid sorting the viewer's history before each LIMIT.
CREATE INDEX idx_video_user_states_progress_asc
ON video_user_states (user_id, last_progress_at_ms ASC NULLS LAST, video_id ASC);
