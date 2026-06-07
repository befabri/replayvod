-- name: GetVideoUserState :one
SELECT * FROM video_user_states WHERE user_id = $1 AND video_id = $2;

-- name: ListVideoUserStatesForVideos :many
SELECT * FROM video_user_states
WHERE user_id = $1 AND video_id = ANY(@video_ids::bigint[]);

-- name: SetVideoWatchLater :one
INSERT INTO video_user_states (user_id, video_id, watch_later, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT(user_id, video_id) DO UPDATE SET
    watch_later = EXCLUDED.watch_later,
    updated_at = NOW()
RETURNING *;

-- name: UpdateVideoWatchProgress :one
INSERT INTO video_user_states (
    user_id, video_id, last_position_seconds, last_progress_at_ms, watched_at, completed_at, updated_at
)
SELECT
    $1, v.id, GREATEST(0::DOUBLE PRECISION, @position_seconds::DOUBLE PRECISION),
    @observed_at_ms::BIGINT, NOW(),
    CASE WHEN @completed::BOOLEAN THEN NOW() ELSE NULL END,
    NOW()
FROM videos v
WHERE v.id = $2
  AND v.deleted_at IS NULL
  AND v.status = 'DONE'
ON CONFLICT(user_id, video_id) DO UPDATE SET
    last_position_seconds = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR EXCLUDED.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN EXCLUDED.last_position_seconds
        ELSE video_user_states.last_position_seconds
    END,
    last_progress_at_ms = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR EXCLUDED.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN EXCLUDED.last_progress_at_ms
        ELSE video_user_states.last_progress_at_ms
    END,
    watched_at = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR EXCLUDED.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN COALESCE(video_user_states.watched_at, EXCLUDED.watched_at)
        ELSE video_user_states.watched_at
    END,
    completed_at = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR EXCLUDED.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN COALESCE(EXCLUDED.completed_at, video_user_states.completed_at)
        ELSE video_user_states.completed_at
    END,
    updated_at = CASE
        WHEN video_user_states.last_progress_at_ms IS NULL
          OR EXCLUDED.last_progress_at_ms >= video_user_states.last_progress_at_ms
        THEN NOW()
        ELSE video_user_states.updated_at
    END
RETURNING *;
