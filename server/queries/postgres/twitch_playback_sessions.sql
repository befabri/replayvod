-- name: GetTwitchPlaybackSession :one
SELECT * FROM twitch_playback_sessions WHERE id = 1;

-- name: SaveTwitchPlaybackSession :exec
INSERT INTO twitch_playback_sessions (id, twitch_user_id, twitch_login, encrypted_token, expires_at, checked_at)
VALUES (1, $1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET twitch_user_id = EXCLUDED.twitch_user_id,
    twitch_login = EXCLUDED.twitch_login, encrypted_token = EXCLUDED.encrypted_token,
    expires_at = EXCLUDED.expires_at, checked_at = EXCLUDED.checked_at, needs_reconnect = FALSE;

-- name: UpdateTwitchPlaybackSessionValidation :exec
UPDATE twitch_playback_sessions SET checked_at = $1, expires_at = $2, needs_reconnect = $3
WHERE id = 1 AND encrypted_token = $4 AND needs_reconnect = FALSE;

-- name: DeleteTwitchPlaybackSession :exec
DELETE FROM twitch_playback_sessions WHERE id = 1;
