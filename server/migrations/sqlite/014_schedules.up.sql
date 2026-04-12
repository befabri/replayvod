CREATE TABLE IF NOT EXISTS download_schedules (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    broadcaster_id      TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    requested_by        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    quality             TEXT NOT NULL DEFAULT 'HIGH'
                        CHECK (quality IN ('LOW', 'MEDIUM', 'HIGH')),
    has_min_viewers     INTEGER NOT NULL DEFAULT 0,
    min_viewers         INTEGER,
    has_categories      INTEGER NOT NULL DEFAULT 0,
    has_tags            INTEGER NOT NULL DEFAULT 0,
    is_delete_rediff    INTEGER NOT NULL DEFAULT 0,
    time_before_delete  INTEGER,
    is_disabled         INTEGER NOT NULL DEFAULT 0,
    last_triggered_at   TEXT,
    trigger_count       INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (broadcaster_id, requested_by),
    CHECK ((has_min_viewers = 0) OR (min_viewers IS NOT NULL AND min_viewers >= 0)),
    CHECK ((is_delete_rediff = 0) OR (time_before_delete IS NOT NULL AND time_before_delete > 0))
);

CREATE INDEX IF NOT EXISTS idx_schedules_broadcaster_id ON download_schedules (broadcaster_id);
CREATE INDEX IF NOT EXISTS idx_schedules_requested_by ON download_schedules (requested_by);
CREATE INDEX IF NOT EXISTS idx_schedules_active
    ON download_schedules (broadcaster_id)
    WHERE is_disabled = 0;

CREATE TABLE IF NOT EXISTS download_schedule_categories (
    schedule_id INTEGER NOT NULL REFERENCES download_schedules(id) ON DELETE CASCADE,
    category_id TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (schedule_id, category_id)
);

CREATE TABLE IF NOT EXISTS download_schedule_tags (
    schedule_id INTEGER NOT NULL REFERENCES download_schedules(id) ON DELETE CASCADE,
    tag_id      INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (schedule_id, tag_id)
);
