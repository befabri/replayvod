-- download_schedules are user-defined auto-record rules matched against
-- incoming stream.online webhooks. Each row is a policy: "when this channel
-- goes live, if viewers/categories/tags match, start a download at this
-- quality; optionally auto-delete after N hours."
CREATE TABLE IF NOT EXISTS download_schedules (
    id                  BIGSERIAL PRIMARY KEY,
    broadcaster_id      TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    requested_by        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    quality             TEXT NOT NULL DEFAULT 'HIGH'
                        CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH')),

    -- Filters. The has_* booleans are the master switch; the corresponding
    -- value columns only matter when their switch is TRUE. This shape
    -- matches v1 and keeps the matching logic simple (read switch → read
    -- value → read junction rows).
    has_min_viewers     BOOLEAN NOT NULL DEFAULT FALSE,
    min_viewers         INTEGER,
    has_categories      BOOLEAN NOT NULL DEFAULT FALSE,
    has_tags            BOOLEAN NOT NULL DEFAULT FALSE,

    -- Retention policy. time_before_delete is in hours; NULL means keep
    -- forever even when is_delete_rediff=TRUE (which would be a config
    -- bug, but we don't enforce it here — the scheduler's retention task
    -- logs and skips schedules with that shape).
    is_delete_rediff    BOOLEAN NOT NULL DEFAULT FALSE,
    time_before_delete  INTEGER,

    is_disabled         BOOLEAN NOT NULL DEFAULT FALSE,

    -- Operational metadata. Without last_triggered_at / trigger_count the
    -- dashboard can tell you a schedule exists but not whether it's doing
    -- anything — the most common operator question. These are cheap
    -- single-row updates on each match, so they don't hurt write load.
    last_triggered_at   TIMESTAMPTZ,
    trigger_count       BIGINT NOT NULL DEFAULT 0,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One schedule per user per channel. Same constraint as v1. If a user
    -- needs multiple policies for the same channel, we'd add a named
    -- column; that's not a current requirement.
    UNIQUE (broadcaster_id, requested_by),

    -- Defensive CHECKs: filter value must be set when its switch is on,
    -- and vice versa. Prevents "has_min_viewers=TRUE, min_viewers=NULL"
    -- silently never matching anything.
    CHECK ((has_min_viewers = FALSE) OR (min_viewers IS NOT NULL AND min_viewers >= 0)),
    CHECK ((is_delete_rediff = FALSE) OR (time_before_delete IS NOT NULL AND time_before_delete > 0))
);

CREATE INDEX IF NOT EXISTS idx_schedules_broadcaster_id ON download_schedules (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_schedules_requested_by ON download_schedules (requested_by);
-- Partial index: the hot path is "find enabled schedules for this channel
-- when a stream.online arrives." Filtering on is_disabled=FALSE at index
-- time keeps lookups O(log active_schedules) even if disabled schedules
-- accumulate historically.
CREATE INDEX IF NOT EXISTS idx_schedules_active ON download_schedules (broadcaster_id) WHERE is_disabled = FALSE;

-- Junction: a schedule's category filter is a set of category IDs. When
-- has_categories=TRUE the matcher requires the stream's current category
-- to be in this set.
CREATE TABLE IF NOT EXISTS download_schedule_categories (
    schedule_id BIGINT NOT NULL REFERENCES download_schedules(id) ON DELETE CASCADE,
    category_id TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (schedule_id, category_id)
);

CREATE TABLE IF NOT EXISTS download_schedule_tags (
    schedule_id BIGINT NOT NULL REFERENCES download_schedules(id) ON DELETE CASCADE,
    tag_id      BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (schedule_id, tag_id)
);
