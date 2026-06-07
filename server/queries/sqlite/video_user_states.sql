-- name: GetVideoUserState :one
SELECT * FROM video_user_states WHERE user_id = ? AND video_id = ?;

-- name: ListVideoUserStatesForVideos :many
SELECT * FROM video_user_states
WHERE user_id = ? AND video_id IN (sqlc.slice('video_ids'));

-- name: SetVideoWatchLater :one
INSERT INTO video_user_states (user_id, video_id, watch_later, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(user_id, video_id) DO UPDATE SET
    watch_later = excluded.watch_later,
    updated_at = datetime('now')
RETURNING *;

-- name: UpdateVideoWatchProgress :one
INSERT INTO video_user_states (
    user_id, video_id, last_position_seconds, last_progress_at_ms, watched_at, completed_at, updated_at
)
SELECT
    ?, v.id, MAX(0, CAST(@position_seconds AS REAL)),
    CAST(@observed_at_ms AS INTEGER), datetime('now'),
    CASE WHEN CAST(@completed AS INTEGER) != 0 THEN datetime('now') ELSE NULL END,
    datetime('now')
FROM videos v
WHERE v.id = ?
  AND v.deleted_at IS NULL
  AND v.status = 'DONE'
ON CONFLICT(user_id, video_id) DO UPDATE SET
    last_position_seconds = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR excluded.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN excluded.last_position_seconds
        ELSE video_user_states.last_position_seconds
    END,
    last_progress_at_ms = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR excluded.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN excluded.last_progress_at_ms
        ELSE video_user_states.last_progress_at_ms
    END,
    watched_at = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR excluded.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN COALESCE(video_user_states.watched_at, excluded.watched_at)
        ELSE video_user_states.watched_at
    END,
    completed_at = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR excluded.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN COALESCE(excluded.completed_at, video_user_states.completed_at)
        ELSE video_user_states.completed_at
    END,
    updated_at = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR excluded.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN datetime('now')
        ELSE video_user_states.updated_at
    END
RETURNING *;
