DROP INDEX IF EXISTS idx_event_logs_search_vector;
ALTER TABLE event_logs DROP COLUMN IF EXISTS search_vector;
