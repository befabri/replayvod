CREATE INDEX IF NOT EXISTS idx_schedule_requests_created ON schedule_requests (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_schedule_requests_user_created ON schedule_requests (requested_by, created_at DESC, id DESC);
