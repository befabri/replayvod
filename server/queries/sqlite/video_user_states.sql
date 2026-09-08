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
-- Progress writes are ordered by the server clock (@progress_at_ms), so
-- devices with skewed clocks cannot shadow each other. watched_at marks the
-- recording as started once the position reaches the smaller of
-- @started_seconds and @started_fraction of the duration, or on completion;
-- both stamps are kept once set. The params subquery binds each argument
-- once: sqlite numbers every placeholder separately.
INSERT INTO video_user_states (
    user_id, video_id, last_position_seconds, last_progress_at_ms, progress_revision, watched_at, completed_at, updated_at
)
SELECT
    ?, v.id, MAX(0, p.position_seconds),
    p.progress_at_ms, 1,
    CASE
        WHEN p.completed != 0
          OR p.position_seconds >= CASE
              WHEN v.duration_seconds > 0
              THEN MIN(p.started_seconds, v.duration_seconds * p.started_fraction)
              ELSE p.started_seconds
          END
        THEN datetime('now') ELSE NULL
    END,
    CASE WHEN p.completed != 0 THEN datetime('now') ELSE NULL END,
    datetime('now')
FROM videos v
CROSS JOIN (
    SELECT
        CAST(@position_seconds AS REAL) AS position_seconds,
        CAST(@progress_at_ms AS INTEGER) AS progress_at_ms,
        CAST(@completed AS INTEGER) AS completed,
        CAST(@started_seconds AS REAL) AS started_seconds,
        CAST(@started_fraction AS REAL) AS started_fraction
) AS p
WHERE v.id = ?
  AND v.deleted_at IS NULL
  AND v.status = 'DONE'
ON CONFLICT(user_id, video_id) DO UPDATE SET
    progress_revision = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN video_user_states.progress_revision + 1 ELSE video_user_states.progress_revision END,
    last_position_seconds = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN excluded.last_position_seconds ELSE video_user_states.last_position_seconds END,
    last_progress_at_ms = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN excluded.last_progress_at_ms ELSE video_user_states.last_progress_at_ms END,
    watched_at = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN COALESCE(video_user_states.watched_at, excluded.watched_at) ELSE video_user_states.watched_at END,
    completed_at = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN COALESCE(excluded.completed_at, video_user_states.completed_at) ELSE video_user_states.completed_at END,
    updated_at = CASE WHEN excluded.last_progress_at_ms >= COALESCE(video_user_states.last_progress_at_ms, 0) THEN datetime('now') ELSE video_user_states.updated_at END
RETURNING *;

-- name: ListContinueWatchingVideos :many
-- Recordings the user started and has not played to the end, most recently
-- watched first. The dashboard applies its resume policy on top.
SELECT v.* FROM videos v
INNER JOIN video_user_states vus
    ON vus.video_id = v.id AND vus.user_id = CAST(@user_id AS TEXT)
WHERE v.deleted_at IS NULL
  AND v.status = 'DONE'
  AND vus.watched_at IS NOT NULL
  AND vus.last_position_seconds > 0
  AND (v.duration_seconds IS NULL OR v.duration_seconds <= 0
       OR vus.last_position_seconds < v.duration_seconds - 1)
ORDER BY vus.last_progress_at_ms DESC, v.id DESC
LIMIT CAST(@row_limit AS INTEGER);
