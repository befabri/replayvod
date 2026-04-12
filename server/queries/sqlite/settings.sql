-- name: GetSettings :one
SELECT * FROM settings WHERE user_id = ?;

-- name: UpsertSettings :one
INSERT INTO settings (user_id, timezone, datetime_format, language)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id) DO UPDATE
SET timezone        = excluded.timezone,
    datetime_format = excluded.datetime_format,
    language        = excluded.language,
    updated_at      = datetime('now')
RETURNING *;
