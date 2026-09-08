-- name: GetTwitchPlaybackSession :one
SELECT * FROM twitch_playback_sessions WHERE id = 1;

-- name: SaveTwitchPlaybackSession :exec
INSERT INTO twitch_playback_sessions (id, twitch_user_id, twitch_login, encrypted_token, expires_at, checked_at)
VALUES (1, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET twitch_user_id = excluded.twitch_user_id,
    twitch_login = excluded.twitch_login, encrypted_token = excluded.encrypted_token,
    expires_at = excluded.expires_at, checked_at = excluded.checked_at, needs_reconnect = 0;

-- name: UpdateTwitchPlaybackSessionValidation :exec
UPDATE twitch_playback_sessions SET checked_at = ?, expires_at = ?, needs_reconnect = ?
WHERE id = 1 AND encrypted_token = ? AND needs_reconnect = 0;

-- name: DeleteTwitchPlaybackSession :exec
DELETE FROM twitch_playback_sessions WHERE id = 1;
