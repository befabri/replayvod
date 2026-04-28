-- Append-only event log of channel.update observations attached to a
-- recording. One row per RecordChannelUpdate / LinkInitialVideoMetadata
-- call, capturing every observed dimension (title, category) under a
-- single occurred_at so the dashboard timeline reads as merged events
-- without inferring "same event" from timestamp coincidence on the
-- span tables.
--
-- title_id / category_id are nullable: an event may carry only one of
-- the two when a webhook delivers a partial update. The CHECK forbids
-- empty events. ON DELETE RESTRICT on titles / categories matches
-- existing junction-table behavior — we don't expect titles or
-- categories to be hard-deleted.
CREATE TABLE video_metadata_changes (
    id           BIGSERIAL PRIMARY KEY,
    video_id     BIGINT      NOT NULL REFERENCES videos(id)     ON DELETE CASCADE,
    occurred_at  TIMESTAMPTZ NOT NULL,
    title_id     BIGINT               REFERENCES titles(id)     ON DELETE RESTRICT,
    category_id  TEXT                 REFERENCES categories(id) ON DELETE RESTRICT,
    CONSTRAINT video_metadata_changes_at_least_one
        CHECK (title_id IS NOT NULL OR category_id IS NOT NULL)
);

CREATE INDEX idx_video_metadata_changes_video_id_occurred_at
    ON video_metadata_changes (video_id, occurred_at, id);
