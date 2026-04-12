-- Full-text search on event_logs, PG-only.
--
-- search_vector is a GENERATED column over (message, event_type, domain)
-- so inserts don't need an application-side tsvector_update_trigger; the
-- column is always in sync with the source fields. The GIN index is the
-- standard tsvector companion — B-tree can't index a tsvector.
--
-- 'simple' dictionary (not english/french) is deliberate: event log
-- messages are a mix of machine strings (job IDs, broadcaster logins,
-- status enums) and human prose, so stemming would more often hurt than
-- help. Exact-word matches with prefix wildcards (to_tsquery's `:*`) are
-- what operators actually run against this.
--
-- SQLite has no tsvector — its corresponding 025 migration is a no-op.
-- The repository's FullTextSearcher interface is PG-only; services that
-- need full-text type-assert on the repo and fall back to LIKE-scoped
-- queries when the assertion fails.
ALTER TABLE event_logs
    ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(message, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(event_type, '')), 'B') ||
        setweight(to_tsvector('simple', coalesce(domain, '')), 'C')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_event_logs_search_vector
    ON event_logs USING GIN (search_vector);
