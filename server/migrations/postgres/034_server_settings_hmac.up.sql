-- Adds the EventSub HMAC secret to the single-row server_settings table. Empty
-- means "not generated yet"; the server fills it on first boot (see
-- internal/secrets). The secret is managed entirely here, not via the
-- environment. Kept in this table rather than a key/value bag so it inherits
-- the same typed, redactable, boot-time treatment as the other server settings.
ALTER TABLE server_settings ADD COLUMN hmac_secret TEXT NOT NULL DEFAULT '';
