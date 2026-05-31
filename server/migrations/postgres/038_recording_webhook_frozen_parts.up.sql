-- Freeze a delivery's part metadata on its first build, before retention can
-- delete the video's parts. The serialized part list (paths, sizes, indices)
-- carries no signed download URLs: those are time-limited and re-minted fresh on
-- every delivery attempt, so a late retry never ships an expired URL. Empty
-- string means the delivery has not been built yet.
ALTER TABLE recording_webhook_deliveries
    ADD COLUMN frozen_parts TEXT NOT NULL DEFAULT '';
