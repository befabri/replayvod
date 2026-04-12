-- event_logs is the append-only audit trail of app-side events. Distinct
-- from webhook_events (which records incoming Twitch payloads) and
-- fetch_logs (which records outgoing Helix calls): event_logs captures
-- things the app itself does — "task run succeeded", "auto-download
-- triggered", "subscription revoked via dashboard" — so operators can
-- reconstruct what happened during an incident without tailing the
-- slog output.
--
-- domain scopes events to a subsystem (task, schedule, download,
-- eventsub, auth, system). event_type is the specific event name
-- within that domain. data carries optional JSON context — kept small;
-- retention task prunes old rows, not just the payload column.
CREATE TABLE IF NOT EXISTS event_logs (
    id              BIGSERIAL PRIMARY KEY,
    domain          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    -- severity drives dashboard highlighting and retention — errors
    -- stay longer than info. CHECK keeps it a closed set; extending
    -- requires a migration, which is deliberate.
    severity        TEXT NOT NULL DEFAULT 'info'
                    CHECK (severity IN ('debug', 'info', 'warn', 'error')),

    message         TEXT NOT NULL,

    -- actor is who/what triggered the event: a user ID for
    -- dashboard-initiated actions, NULL for system-triggered (scheduler
    -- tick, incoming webhook).
    actor_user_id   TEXT REFERENCES users(id) ON DELETE SET NULL,

    -- Free-form JSON context. Parsed by the dashboard for structured
    -- filtering; NULL when the message text is enough.
    data            JSONB,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_event_logs_created_at ON event_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_event_logs_domain_type ON event_logs (domain, event_type);
CREATE INDEX IF NOT EXISTS idx_event_logs_severity ON event_logs (severity) WHERE severity IN ('warn', 'error');
