-- Nothing ever wrote or read per-video tags, and these webhook indexes served
-- only list and stuck-event queries that no longer exist while still costing
-- a write on every delivery.
DROP TABLE video_tags;
DROP INDEX idx_webhook_events_broadcaster;
DROP INDEX idx_webhook_events_type;
DROP INDEX idx_webhook_events_received_status;
