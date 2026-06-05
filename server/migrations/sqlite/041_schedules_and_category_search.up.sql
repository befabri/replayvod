-- Combined unreleased migration: schedule recording modes (video/audio +
-- force_h264), the Twitch category-search cache, the category browse lookup
-- index, and the global schedule pause switch. Folded into one file because
-- none of these shipped separately.
-- See postgres/041_schedules_and_category_search.up.sql for full design notes.

-- 1. recording_type lets scheduled auto-record rules request video or audio mode.
ALTER TABLE download_schedules ADD COLUMN recording_type TEXT NOT NULL DEFAULT 'video'
    CHECK (recording_type IN ('video', 'audio'));
ALTER TABLE download_schedules ADD COLUMN force_h264 INTEGER NOT NULL DEFAULT 0
    CHECK (force_h264 IN (0, 1));

-- 2. Twitch category-search cache (IDs stored in Twitch order for stable
-- storage; the search service re-ranks by local relevance before returning).
CREATE TABLE IF NOT EXISTS category_search_cache (
    normalized_query TEXT PRIMARY KEY,
    category_ids     TEXT NOT NULL,
    expires_at       TEXT NOT NULL,
    last_accessed_at TEXT NOT NULL DEFAULT (datetime('now')),
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
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

-- 4. Global auto-download pause switch. When true (1), the schedule processor
-- skips every stream.online auto-download without touching any individual
-- schedule's is_disabled flag, so resuming restores prior state exactly.
ALTER TABLE server_settings ADD COLUMN schedules_paused INTEGER NOT NULL DEFAULT 0;
