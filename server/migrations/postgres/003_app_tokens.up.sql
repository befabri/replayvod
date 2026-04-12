CREATE TABLE IF NOT EXISTS app_access_tokens (
    id          BIGSERIAL PRIMARY KEY,
    token       TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
