CREATE TABLE recording_intents (
 id TEXT PRIMARY KEY,
 broadcaster_id TEXT NOT NULL REFERENCES channels(broadcaster_id) ON DELETE CASCADE,
 params JSONB NOT NULL,
 wait_seconds BIGINT NOT NULL CHECK (wait_seconds > 0),
 status TEXT NOT NULL CHECK (status IN ('active','waiting','stopped','expired')),
 current_job_id TEXT NOT NULL,
 last_stream_id TEXT NOT NULL DEFAULT '',
 wait_until TIMESTAMPTZ,
 stop_requested BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_recording_intents_channel ON recording_intents(broadcaster_id) WHERE status IN ('active','waiting');
CREATE TABLE recording_intent_videos (
 intent_id TEXT NOT NULL REFERENCES recording_intents(id) ON DELETE CASCADE,
 video_id BIGINT NOT NULL UNIQUE REFERENCES videos(id) ON DELETE CASCADE,
 position BIGINT NOT NULL,
 stream_id TEXT,
 PRIMARY KEY (intent_id,position),
 UNIQUE (intent_id,stream_id)
);
