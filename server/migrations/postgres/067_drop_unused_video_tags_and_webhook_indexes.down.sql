CREATE INDEX idx_webhook_events_received_status
    ON webhook_events (received_at)
    WHERE status = 'received';
CREATE INDEX idx_webhook_events_type ON webhook_events (event_type);
CREATE INDEX idx_webhook_events_broadcaster ON webhook_events (broadcaster_id);
CREATE TABLE video_tags (
    video_id BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    tag_id   BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (video_id, tag_id)
);
