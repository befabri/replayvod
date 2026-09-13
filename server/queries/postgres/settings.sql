-- name: GetSettings :one
SELECT * FROM settings WHERE user_id = $1;

-- name: EnsureSettings :exec
INSERT INTO settings (user_id) VALUES ($1)
ON CONFLICT (user_id) DO NOTHING;

-- name: UpsertSettings :one
-- Locale updates preserve playback preferences saved by another request.
INSERT INTO settings (user_id, timezone, datetime_format, language)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET timezone        = EXCLUDED.timezone,
    datetime_format = EXCLUDED.datetime_format,
    language        = EXCLUDED.language,
    updated_at      = NOW()
RETURNING *;

-- name: UpdatePlaybackSettings :one
-- Updating playback must preserve locale preferences, including concurrent saves.
INSERT INTO settings (user_id, resume_min_seconds, resume_end_margin_seconds, resume_end_margin_percent)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE SET
    resume_min_seconds = excluded.resume_min_seconds,
    resume_end_margin_seconds = excluded.resume_end_margin_seconds,
    resume_end_margin_percent = excluded.resume_end_margin_percent,
    updated_at = NOW()
RETURNING *;
