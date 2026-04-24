-- Span tables for title/category history. Each row is one interval
-- during which a video carried a given title or category; a stream
-- that switches title and back produces two rows. Open spans
-- (ended_at IS NULL) track the still-running interval; the
-- CloseOpen* queries stamp ended_at at recording termination. The
-- partial unique index keeps at most one open span per (video,
-- title) / (video, category) pair under concurrent writers.
CREATE TABLE video_title_spans (
    id                BIGSERIAL PRIMARY KEY,
    video_id          BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    title_id          BIGINT NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    started_at        TIMESTAMPTZ NOT NULL,
    ended_at          TIMESTAMPTZ,
    duration_seconds  DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE INDEX idx_video_title_spans_video_id_started_at
    ON video_title_spans (video_id, started_at);

CREATE INDEX idx_video_title_spans_video_id_open
    ON video_title_spans (video_id)
    WHERE ended_at IS NULL;

CREATE UNIQUE INDEX idx_video_title_spans_unique_open
    ON video_title_spans (video_id, title_id)
    WHERE ended_at IS NULL;

CREATE TABLE video_category_spans (
    id                BIGSERIAL PRIMARY KEY,
    video_id          BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    category_id       TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    started_at        TIMESTAMPTZ NOT NULL,
    ended_at          TIMESTAMPTZ,
    duration_seconds  DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE INDEX idx_video_category_spans_video_id_started_at
    ON video_category_spans (video_id, started_at);

CREATE INDEX idx_video_category_spans_video_id_open
    ON video_category_spans (video_id)
    WHERE ended_at IS NULL;

CREATE UNIQUE INDEX idx_video_category_spans_unique_open
    ON video_category_spans (video_id, category_id)
    WHERE ended_at IS NULL;
