-- name: GetSettings :one
SELECT * FROM settings WHERE user_id = $1;

-- name: UpsertSettings :one
-- Called on first access and on every update. Defaults (UTC / ISO /
-- en) come from the column defaults when the caller passes empty
-- strings, but we expect callers to pass concrete values — validation
-- lives at the tRPC boundary.
INSERT INTO settings (user_id, timezone, datetime_format, language)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET timezone        = EXCLUDED.timezone,
    datetime_format = EXCLUDED.datetime_format,
    language        = EXCLUDED.language,
    updated_at      = NOW()
RETURNING *;
