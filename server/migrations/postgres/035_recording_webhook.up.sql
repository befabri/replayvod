-- Adds the outbound recording webhook config to the single-row server_settings
-- table. When enabled, the server fires a signed HTTP POST whenever a recording
-- reaches a terminal state (recording.completed / recording.failed). It is a
-- generic relay primitive: a self-hoster points it at any receiver (a media
-- server refresh, a notifier, a post-processing script) with zero app coupling.
--
-- The signing secret is managed entirely here, exactly like hmac_secret: empty
-- means "not generated yet"; the owner UI fills it (auto-generated when blank)
-- and the receiver verifies deliveries against it. recording_webhook_events is a
-- comma-separated subset of {recording.completed, recording.failed}; empty means
-- "all terminal events".
ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS recording_webhook_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS recording_webhook_url     TEXT    NOT NULL DEFAULT '';
ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS recording_webhook_secret  TEXT    NOT NULL DEFAULT '';
ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS recording_webhook_events  TEXT    NOT NULL DEFAULT '';
