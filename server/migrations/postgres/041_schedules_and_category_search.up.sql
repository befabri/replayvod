-- Combined unreleased migration: schedule recording modes (video/audio +
-- force_h264), the Twitch category-search cache, the category browse lookup
-- index, and the global schedule pause switch. Folded into one file because
-- none of these shipped separately.

-- 1. recording_type lets scheduled auto-record rules request video or audio mode.
-- Existing schedules backfill to video so current behavior is preserved.
ALTER TABLE download_schedules ADD COLUMN IF NOT EXISTS recording_type TEXT NOT NULL DEFAULT 'video'
    CHECK (recording_type IN ('video', 'audio'));
ALTER TABLE download_schedules ADD COLUMN IF NOT EXISTS force_h264 BOOLEAN NOT NULL DEFAULT FALSE;

-- 2. Cache Twitch category-search results by normalized query. The categories
-- table remains the source of truth; this table stores the matching category IDs
-- so schedule pickers do not burn Helix quota on repeated searches. IDs are
-- stored in Twitch result order for stable storage only — the search service
-- re-ranks merged local + cached + remote rows by local query relevance before
-- returning, so this stored order is not the order callers ultimately see.
CREATE TABLE IF NOT EXISTS category_search_cache (
    normalized_query TEXT PRIMARY KEY,
    category_ids     JSONB NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    last_accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_category_search_cache_expires_at
ON category_search_cache (expires_at);

CREATE INDEX IF NOT EXISTS idx_category_search_cache_lru
ON category_search_cache (last_accessed_at, updated_at, normalized_query);

-- 3. Reverse lookup for category browse and video.byCategory. The primary key
-- is (video_id, category_id), which is right for writes and per-video lookups;
-- browse paths need category_id first.
CREATE INDEX IF NOT EXISTS idx_video_categories_category_id_video_id
ON video_categories (category_id, video_id);

-- 4. Global auto-download pause switch. When true, the schedule processor skips
-- every stream.online auto-download without touching any individual schedule's
-- is_disabled flag, so resuming restores each schedule's prior state exactly.
ALTER TABLE server_settings ADD COLUMN IF NOT EXISTS schedules_paused BOOLEAN NOT NULL DEFAULT FALSE;
