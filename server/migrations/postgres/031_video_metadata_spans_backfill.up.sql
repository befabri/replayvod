CREATE TABLE IF NOT EXISTS video_title_spans (
    id                BIGSERIAL PRIMARY KEY,
    video_id          BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    title_id          BIGINT NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    started_at        TIMESTAMPTZ NOT NULL,
    ended_at          TIMESTAMPTZ,
    duration_seconds  DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_video_title_spans_video_id_started_at
    ON video_title_spans (video_id, started_at);

CREATE INDEX IF NOT EXISTS idx_video_title_spans_video_id_open
    ON video_title_spans (video_id)
    WHERE ended_at IS NULL;

CREATE TABLE IF NOT EXISTS video_category_spans (
    id                BIGSERIAL PRIMARY KEY,
    video_id          BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    category_id       TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    started_at        TIMESTAMPTZ NOT NULL,
    ended_at          TIMESTAMPTZ,
    duration_seconds  DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_video_category_spans_video_id_started_at
    ON video_category_spans (video_id, started_at);

CREATE INDEX IF NOT EXISTS idx_video_category_spans_video_id_open
    ON video_category_spans (video_id)
    WHERE ended_at IS NULL;
