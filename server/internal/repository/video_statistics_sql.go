package repository

import "fmt"

// BuildVideoStatsTotalsQuery returns library and viewer totals from one database
// snapshot. Its SQL bypasses sqlc because SQLite aggregate generation is unreliable.
func BuildVideoStatsTotalsQuery(userID string, d VideoPageDialect) (string, []any) {
	b := &videoPageBuilder{d: d}
	cutoff := "datetime('now', '-7 days')"
	durationType := "REAL"
	if d.Postgres {
		cutoff = "now() - interval '7 days'"
		durationType = "DOUBLE PRECISION"
	}
	query := fmt.Sprintf(`WITH viewer_counts AS (
  SELECT
    COUNT(*) FILTER (WHERE progress.watch_later) AS watch_later,
    COUNT(*) FILTER (WHERE videos.status = 'DONE' AND progress.watched_at IS NOT NULL) AS watched,
    COUNT(*) FILTER (WHERE 1=1 %s) AS continue_watching
  FROM video_user_states progress
  INNER JOIN videos ON videos.id = progress.video_id
  LEFT JOIN settings resume_settings ON resume_settings.user_id = progress.user_id
  WHERE progress.user_id = %s AND %s <> '' AND videos.deleted_at IS NULL
), library_totals AS (
  SELECT
    COUNT(*) FILTER (WHERE status = 'DONE') AS total,
    CAST(COALESCE(SUM(size_bytes) FILTER (WHERE status = 'DONE'), 0) AS BIGINT) AS total_size,
    CAST(COALESCE(SUM(duration_seconds) FILTER (WHERE status = 'DONE'), 0) AS %s) AS total_duration,
    COUNT(*) FILTER (WHERE start_download_at >= %s) AS this_week,
    COUNT(*) FILTER (WHERE completion_kind = 'partial' OR truncated) AS incomplete,
    COUNT(DISTINCT broadcaster_id) AS channels
  FROM videos WHERE deleted_at IS NULL
)
SELECT total, total_size, total_duration, this_week, incomplete, channels,
  (SELECT COUNT(*) FROM videos WHERE deleted_at IS NOT NULL) AS removed,
  watch_later,
  CASE WHEN %s <> '' THEN total - watched ELSE 0 END AS unwatched,
  continue_watching
FROM library_totals CROSS JOIN viewer_counts`,
		b.continueWatchingEligibilitySQL(), b.phText(userID), b.phText(userID),
		durationType, cutoff, b.phText(userID))
	return query, b.args
}
