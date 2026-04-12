-- subscriptions is our local mirror of Twitch EventSub subscriptions we
-- own. One row per sub Twitch confirms. Fields directly mirror the EventSub
-- API shape (id/status/type/version/condition/transport/cost/created_at)
-- plus our own bookkeeping (revoked_at, broadcaster_id denorm).
--
-- Not hard-deleting: Twitch revokes subs on several conditions (moderator
-- removal, auth revocation, delivery failure), and we want the audit trail
-- to survive revocation. Soft-delete via revoked_at + revoked_reason lets
-- us distinguish "never existed" from "existed and was revoked for X."
CREATE TABLE IF NOT EXISTS subscriptions (
    id                  TEXT PRIMARY KEY,               -- Twitch subscription UUID
    status              TEXT NOT NULL,                  -- enabled, webhook_callback_verification_pending, authorization_revoked, etc.
    type                TEXT NOT NULL,                  -- stream.online, channel.update, etc.
    version             TEXT NOT NULL,                  -- Twitch sub version ("1", "2", ...); different versions have different payload shapes
    cost                INTEGER NOT NULL DEFAULT 0,

    -- condition is type-specific JSON. Stored as JSONB so we can index on
    -- inner fields later (e.g., GIN on broadcaster_user_id for lookup).
    -- For stream.online: {"broadcaster_user_id": "12345"}.
    condition           JSONB NOT NULL,

    -- broadcaster_id is denormalized from condition for indexed lookup.
    -- Not all EventSub types carry a broadcaster (drop.entitlement.grant
    -- keys on organization_id); nullable for those.
    broadcaster_id      TEXT REFERENCES channels(broadcaster_id) ON DELETE CASCADE,

    -- transport describes how Twitch delivers events. method is always
    -- "webhook" in v2 but Twitch supports "websocket" and future methods;
    -- storing the field means we don't have to migrate if we add one.
    -- callback is our public URL at subscription time — if we redeploy to
    -- a new domain, the historical record shows where Twitch was pointing.
    transport_method    TEXT NOT NULL,
    transport_callback  TEXT NOT NULL,

    twitch_created_at   TIMESTAMPTZ NOT NULL,           -- Twitch's created_at (for drift detection)
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Soft-delete. revoked_at is NULL for active subs, set when Twitch
    -- sends us a revocation message (or we explicitly DELETE via API).
    revoked_at          TIMESTAMPTZ,
    revoked_reason      TEXT
);

-- At most one ACTIVE subscription per (broadcaster, type) — Twitch rejects
-- duplicates server-side, this UNIQUE enforces it locally too. Partial so
-- that a revoked subscription doesn't block creating a new one.
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_active_unique
    ON subscriptions (broadcaster_id, type)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_subscriptions_broadcaster ON subscriptions (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions (status);
CREATE INDEX IF NOT EXISTS idx_subscriptions_type ON subscriptions (type);

-- eventsub_snapshots records the aggregate state returned by the Helix
-- GET /eventsub/subscriptions endpoint. One row per poll (scheduler runs
-- every N minutes). Used for:
--   1. Charting cost/quota usage over time on the system dashboard
--   2. Reconstructing "what was running when this incident happened"
CREATE TABLE IF NOT EXISTS eventsub_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    total           INTEGER NOT NULL DEFAULT 0,         -- Twitch's total: count of subscriptions
    total_cost      INTEGER NOT NULL DEFAULT 0,         -- Twitch's total_cost: quota used
    max_total_cost  INTEGER NOT NULL DEFAULT 0,         -- Twitch's max_total_cost: quota ceiling
    fetched_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_eventsub_snapshots_fetched_at
    ON eventsub_snapshots (fetched_at DESC);

-- snapshot_subscriptions links a snapshot to the subs that existed at
-- that moment, with each sub's status/cost AT snapshot time. This preserves
-- state even after a sub is later revoked or its cost changes: "what was
-- my quota breakdown on Tuesday at 3pm?" stays answerable.
--
-- cost_at_snapshot and status_at_snapshot are denormalized on purpose —
-- without them a snapshot that predates a status change shows the CURRENT
-- status, which is the wrong answer for historical analysis.
CREATE TABLE IF NOT EXISTS snapshot_subscriptions (
    snapshot_id         BIGINT NOT NULL REFERENCES eventsub_snapshots(id) ON DELETE CASCADE,
    subscription_id     TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    cost_at_snapshot    INTEGER NOT NULL,
    status_at_snapshot  TEXT NOT NULL,
    PRIMARY KEY (snapshot_id, subscription_id)
);

CREATE INDEX IF NOT EXISTS idx_snapshot_subs_subscription ON snapshot_subscriptions (subscription_id);
