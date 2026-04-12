-- settings stores per-user preferences. One row per user (user_id is
-- both PK and FK), created on first access, never auto-deleted during
-- a user's lifetime. ON DELETE CASCADE so a user removal cleans up
-- their settings row without orphaning it.
--
-- Scope is deliberately narrow: display preferences the dashboard
-- applies client-side. Server-side preferences (notification opt-ins,
-- per-user quota overrides) would live here too when they appear, but
-- none exist today.
CREATE TABLE IF NOT EXISTS settings (
    user_id          TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    -- IANA timezone name (e.g. "Europe/Paris"). Validated at the tRPC
    -- boundary because the DB can't reasonably enforce it; a malformed
    -- value round-trips safely but the dashboard falls back to UTC.
    timezone         TEXT NOT NULL DEFAULT 'UTC',

    -- datetime_format is a dashboard preset key ("ISO", "EU", "US",
    -- etc.), not a strftime pattern. Keeps the enum small and avoids
    -- users entering patterns that break the formatter.
    datetime_format  TEXT NOT NULL DEFAULT 'ISO',

    -- ISO 639-1 two-letter code. "en" and "fr" are the only values
    -- with locale files today; others fall back to English at render.
    language         TEXT NOT NULL DEFAULT 'en',

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
