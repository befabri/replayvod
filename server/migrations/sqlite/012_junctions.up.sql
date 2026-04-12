CREATE TABLE IF NOT EXISTS stream_categories (
    stream_id   TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    category_id TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (stream_id, category_id)
);

CREATE TABLE IF NOT EXISTS video_categories (
    video_id    INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    category_id TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (video_id, category_id)
);

CREATE TABLE IF NOT EXISTS stream_tags (
    stream_id TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    tag_id    INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (stream_id, tag_id)
);

CREATE TABLE IF NOT EXISTS video_tags (
    video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (video_id, tag_id)
);

CREATE TABLE IF NOT EXISTS stream_titles (
    stream_id  TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    title_id   INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    PRIMARY KEY (stream_id, title_id)
);

CREATE TABLE IF NOT EXISTS video_titles (
    video_id   INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    title_id   INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    PRIMARY KEY (video_id, title_id)
);

CREATE TABLE IF NOT EXISTS video_requests (
    video_id     INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (video_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_video_requests_user_id ON video_requests (user_id);
