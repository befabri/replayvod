CREATE TABLE IF NOT EXISTS video_user_states (
    user_id               TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    video_id              BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    watch_later           BOOLEAN NOT NULL DEFAULT FALSE,
    last_position_seconds DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (last_position_seconds >= 0),
    last_progress_at_ms   BIGINT,
    watched_at            TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, video_id)
);

CREATE INDEX IF NOT EXISTS idx_video_user_states_user_watch_later
ON video_user_states (user_id, watch_later, updated_at, video_id);

CREATE INDEX IF NOT EXISTS idx_video_user_states_user_watched_at
ON video_user_states (user_id, watched_at, video_id);

CREATE TABLE IF NOT EXISTS channel_user_states (
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    broadcaster_id TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
    favorite       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, broadcaster_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_user_states_user_favorite
ON channel_user_states (user_id, favorite, updated_at, broadcaster_id);
