-- name: GetSettings :one
SELECT * FROM settings WHERE user_id = ?;

-- name: EnsureSettings :exec
INSERT INTO settings (user_id) VALUES (?)
ON CONFLICT (user_id) DO NOTHING;

-- name: UpsertSettings :one
INSERT INTO settings (user_id, timezone, datetime_format, language)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id) DO UPDATE
SET timezone        = excluded.timezone,
    datetime_format = excluded.datetime_format,
    language        = excluded.language,
    updated_at      = datetime('now')
RETURNING *;

-- name: UpdatePlaybackSettings :one
-- Updating playback must preserve locale preferences, including concurrent saves.
INSERT INTO settings (user_id, resume_min_seconds, resume_end_margin_seconds, resume_end_margin_percent)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET
    resume_min_seconds = excluded.resume_min_seconds,
    resume_end_margin_seconds = excluded.resume_end_margin_seconds,
    resume_end_margin_percent = excluded.resume_end_margin_percent,
    updated_at = datetime('now')
RETURNING *;
