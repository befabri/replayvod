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
-- Progress writes are ordered by the server clock (@progress_at_ms), so
-- devices with skewed clocks cannot shadow each other. watched_at marks the
-- recording as started once the position reaches the smaller of
-- @started_seconds and @started_fraction of the duration, or on completion;
-- both stamps are kept once set.
INSERT INTO video_user_states (
    user_id, video_id, last_position_seconds, last_progress_at_ms, progress_revision, watched_at, completed_at, updated_at
)
SELECT
    @user_id, v.id, GREATEST(0::DOUBLE PRECISION, @position_seconds::DOUBLE PRECISION),
    @progress_at_ms::BIGINT, 1,
    CASE
        WHEN @completed::BOOLEAN
          OR @position_seconds::DOUBLE PRECISION >= CASE
              WHEN v.duration_seconds > 0
              THEN LEAST(@started_seconds::DOUBLE PRECISION, v.duration_seconds * @started_fraction::DOUBLE PRECISION)
              ELSE @started_seconds::DOUBLE PRECISION
          END
        THEN NOW() ELSE NULL
    END,
    CASE WHEN @completed::BOOLEAN THEN NOW() ELSE NULL END,
    NOW()
FROM videos v
WHERE v.id = @video_id
  AND v.deleted_at IS NULL
  AND v.status = 'DONE'
ON CONFLICT(user_id, video_id) DO UPDATE SET
    progress_revision = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN video_user_states.progress_revision + 1 ELSE video_user_states.progress_revision END,
    last_position_seconds = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN EXCLUDED.last_position_seconds ELSE video_user_states.last_position_seconds END,
    last_progress_at_ms = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN EXCLUDED.last_progress_at_ms ELSE video_user_states.last_progress_at_ms END,
    watched_at = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN COALESCE(video_user_states.watched_at, EXCLUDED.watched_at) ELSE video_user_states.watched_at END,
    completed_at = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN COALESCE(EXCLUDED.completed_at, video_user_states.completed_at) ELSE video_user_states.completed_at END,
    updated_at = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN NOW() ELSE video_user_states.updated_at END
RETURNING *;
