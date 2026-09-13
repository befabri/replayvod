package repository

import (
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/resumepolicy"
)

// videosPageColumnsSQL must preserve the column order consumed by both adapters.
const videosPageColumnsSQL = `SELECT
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at, deletion_kind, delete_requested_at,
    recording_type, force_h264, title, completion_kind, truncated,
    trigger_schedule_id, retention_source_schedule_id, retention_window_hours,
    source, twitch_video_id, broadcast_at, next_retry_at
`

// VideoPageDialect supplies the placeholder/cast and timestamp-binding pieces
// that differ between Postgres and SQLite.
type VideoPageDialect struct {
	Postgres bool
	// FormatTime converts a cursor timestamp to the engine's bind value:
	// Postgres binds time.Time directly; SQLite binds the adapter's text form.
	FormatTime func(time.Time) any
}

type videoPageBuilder struct {
	d    VideoPageDialect
	args []any
}

func (b *videoPageBuilder) ph(v any, pgCast string) string {
	b.args = append(b.args, v)
	if !b.d.Postgres {
		return "?"
	}
	if pgCast == "" {
		return fmt.Sprintf("$%d", len(b.args))
	}
	return fmt.Sprintf("$%d::%s", len(b.args), pgCast)
}

func (b *videoPageBuilder) phText(v string) string { return b.ph(v, "text") }

// phTextPtr binds nullable cursor strings; SQLite needs a driver.Value.
func (b *videoPageBuilder) phTextPtr(p *string) string {
	if b.d.Postgres {
		return b.ph(p, "text")
	}
	var v any
	if p != nil {
		v = *p
	}
	return b.ph(v, "")
}

func (b *videoPageBuilder) phFloatPtr(p *float64) string {
	if b.d.Postgres {
		return b.ph(p, "double precision")
	}
	var v any
	if p != nil {
		v = *p
	}
	return b.ph(v, "")
}

func (b *videoPageBuilder) phIntPtr(p *int64) string {
	if b.d.Postgres {
		return b.ph(p, "bigint")
	}
	var v any
	if p != nil {
		v = *p
	}
	return b.ph(v, "")
}

func (b *videoPageBuilder) phID(id int64) string { return b.ph(id, "") }

func (b *videoPageBuilder) phBool(v bool) string {
	if b.d.Postgres {
		return b.ph(v, "boolean")
	}
	n := int64(0)
	if v {
		n = 1
	}
	return b.ph(n, "")
}

func (b *videoPageBuilder) phTime(t time.Time, present bool) string {
	if !present {
		return b.ph(nil, "timestamptz")
	}
	return b.ph(b.d.FormatTime(t.UTC()), "timestamptz")
}

// BuildListVideosPageQuery renders the shared keyset videos-list query for one
// SQL dialect.
func BuildListVideosPageQuery(opts ListVideosOpts, cursor *VideoListPageCursor, d VideoPageDialect) (string, []any) {
	sort, order := NormalizeVideoListSort(opts)
	limit := int64(ListVideosPageQueryLimit(opts.Limit))
	b := &videoPageBuilder{d: d}

	progress := "NULL"
	from := " FROM videos"
	if sort == "last_watched" {
		progress = "progress.last_progress_at_ms"
		join := " LEFT JOIN"
		if opts.ContinueWatchingOnly {
			join = " INNER JOIN"
		}
		from += join + " video_user_states progress ON progress.video_id = videos.id AND progress.user_id = " + b.phText(opts.UserID)
	}
	if opts.ContinueWatchingOnly {
		from += " LEFT JOIN (SELECT user_id, resume_min_seconds, resume_end_margin_seconds, resume_end_margin_percent FROM settings) resume_settings ON resume_settings.user_id = " + b.phText(opts.UserID)
	}
	query := videosPageColumnsSQL + ", " + progress + " AS watch_progress_at_ms" + from + " WHERE 1=1" +
		fmt.Sprintf("\n  AND (%s = '' OR status = %s)", b.phText(opts.Status), b.phText(opts.Status)) +
		b.filtersSQL(opts, sort) +
		b.cursorAndOrderSQL(sort, order, cursor, opts.ContinueWatchingOnly) +
		fmt.Sprintf("\nLIMIT %s", b.phID(limit))
	return query, b.args
}

func (b *videoPageBuilder) filtersSQL(opts ListVideosOpts, sort string) string {
	var scope string
	switch opts.Scope {
	case "removed":
		scope = "\n  AND deleted_at IS NOT NULL"
	case "all":
		scope = ""
	default:
		scope = "\n  AND deleted_at IS NULL"
	}

	// FPS-aware quality match: "<height>p<fps>" e.g. "1080p60".
	fpsCast := "ROUND(selected_fps)::int::text"
	if !b.d.Postgres {
		fpsCast = "CAST(ROUND(selected_fps) AS INTEGER)"
	}
	quality := fmt.Sprintf(
		"\n  AND (%s = '' OR quality = %s OR selected_quality = %s OR selected_quality || 'p' = %s OR (selected_fps IS NOT NULL AND selected_fps > 0 AND selected_quality || 'p' || %s = %s))",
		b.phText(opts.Quality), b.phText(opts.Quality), b.phText(opts.Quality), b.phText(opts.Quality), fpsCast, b.phText(opts.Quality),
	)
	broadcaster := fmt.Sprintf("\n  AND (%s = '' OR broadcaster_id = %s)", b.phText(opts.BroadcasterID), b.phText(opts.BroadcasterID))
	language := fmt.Sprintf("\n  AND (%s = '' OR language = %s)", b.phText(opts.Language), b.phText(opts.Language))
	source := fmt.Sprintf("\n  AND (%s = '' OR source = %s)", b.phText(opts.Source), b.phText(opts.Source))
	kind := fmt.Sprintf("\n  AND (%s = '' OR deletion_kind = %s)", b.phText(opts.DeletionKind), b.phText(opts.DeletionKind))
	durationMin := fmt.Sprintf("\n  AND (%s IS NULL OR duration_seconds >= %s)", b.phFloatPtr(opts.DurationMinSeconds), b.phFloatPtr(opts.DurationMinSeconds))
	durationMax := fmt.Sprintf("\n  AND (%s IS NULL OR duration_seconds < %s)", b.phFloatPtr(opts.DurationMaxSeconds), b.phFloatPtr(opts.DurationMaxSeconds))
	sizeMin := fmt.Sprintf("\n  AND (%s IS NULL OR size_bytes >= %s)", b.phIntPtr(opts.SizeMinBytes), b.phIntPtr(opts.SizeMinBytes))
	sizeMax := fmt.Sprintf("\n  AND (%s IS NULL OR size_bytes < %s)", b.phIntPtr(opts.SizeMaxBytes), b.phIntPtr(opts.SizeMaxBytes))

	nowCutoff := "now() - interval '7 days'"
	if !b.d.Postgres {
		nowCutoff = "datetime('now', '-7 days')"
	}
	window := fmt.Sprintf("\n  AND (%s = '' OR (%s = 'this_week' AND start_download_at >= %s))", b.phText(opts.Window), b.phText(opts.Window), nowCutoff)

	var incomplete string
	if b.d.Postgres {
		incomplete = fmt.Sprintf("\n  AND (NOT %s OR completion_kind = 'partial' OR truncated)", b.phBool(opts.IncompleteOnly))
	} else {
		incomplete = fmt.Sprintf("\n  AND (%s = 0 OR completion_kind = 'partial' OR truncated = 1)", b.phBool(opts.IncompleteOnly))
	}

	// This predicate must agree with ClassifyVideoOutcome.
	outcome := fmt.Sprintf(
		"\n  AND (%s = '' OR (%s = 'completed' AND status = 'DONE')"+
			" OR (%s = 'failed' AND status = 'FAILED' AND completion_kind <> 'cancelled')"+
			" OR (%s = 'cancelled' AND status = 'FAILED' AND completion_kind = 'cancelled'))",
		b.phText(opts.Outcome), b.phText(opts.Outcome), b.phText(opts.Outcome), b.phText(opts.Outcome),
	)

	terminal := fmt.Sprintf("\n  AND (NOT %s OR status IN ('DONE', 'FAILED'))", b.phBool(opts.TerminalOnly))
	watchLater := b.watchLaterSQL(opts)
	unwatched := b.unwatchedSQL(opts)

	return scope + quality + broadcaster + language + source + kind + durationMin + durationMax + sizeMin + sizeMax + window + incomplete + outcome + terminal + watchLater + unwatched + b.continueWatchingSQL(opts, sort)
}

// continueWatchingEligibilitySQL requires the viewer's progress and
// resume_settings aliases; list pages and statistics share this predicate.
func (b *videoPageBuilder) continueWatchingEligibilitySQL() string {
	return fmt.Sprintf(`
  AND videos.deleted_at IS NULL
  AND videos.status = 'DONE'
  AND progress.watched_at IS NOT NULL
  AND progress.last_position_seconds >= COALESCE(resume_settings.resume_min_seconds, %s)
  AND (videos.duration_seconds IS NULL OR videos.duration_seconds <= 0
    OR progress.last_position_seconds < videos.duration_seconds - CASE
      WHEN videos.duration_seconds * COALESCE(resume_settings.resume_end_margin_percent, %s) / 100.0 < COALESCE(resume_settings.resume_end_margin_seconds, %s)
      THEN videos.duration_seconds * COALESCE(resume_settings.resume_end_margin_percent, %s) / 100.0
      ELSE COALESCE(resume_settings.resume_end_margin_seconds, %s) END)`,
		b.ph(resumepolicy.MinSeconds, "double precision"),
		b.ph(resumepolicy.EndMarginPercent, "double precision"), b.ph(resumepolicy.EndMarginSeconds, "double precision"),
		b.ph(resumepolicy.EndMarginPercent, "double precision"), b.ph(resumepolicy.EndMarginSeconds, "double precision"))
}

func (b *videoPageBuilder) continueWatchingSQL(opts ListVideosOpts, sort string) string {
	if !opts.ContinueWatchingOnly {
		return ""
	}
	if sort == "last_watched" {
		return b.continueWatchingEligibilitySQL()
	}
	return " AND EXISTS (SELECT 1 FROM video_user_states progress WHERE progress.video_id = videos.id AND progress.user_id = " + b.phText(opts.UserID) + b.continueWatchingEligibilitySQL() + ")"
}

func (b *videoPageBuilder) watchLaterSQL(opts ListVideosOpts) string {
	if b.d.Postgres {
		return fmt.Sprintf(`
	  AND (
	    NOT %s
	    OR EXISTS (
	      SELECT 1 FROM video_user_states vus
	      WHERE vus.video_id = videos.id
	        AND vus.user_id = %s
	        AND vus.watch_later
	    )
	  )`, b.phBool(opts.WatchLaterOnly), b.phText(opts.UserID))
	}
	return fmt.Sprintf(`
	  AND (
	    %s = 0
	    OR EXISTS (
	      SELECT 1 FROM video_user_states vus
	      WHERE vus.video_id = videos.id
	        AND vus.user_id = %s
	        AND vus.watch_later = 1
	    )
	  )`, b.phBool(opts.WatchLaterOnly), b.phText(opts.UserID))
}

func (b *videoPageBuilder) unwatchedSQL(opts ListVideosOpts) string {
	if b.d.Postgres {
		return fmt.Sprintf(`
	  AND (
	    NOT %s
	    OR (
	      %s <> ''
	      AND status = 'DONE'
	      AND NOT EXISTS (
	        SELECT 1 FROM video_user_states vus
	        WHERE vus.video_id = videos.id
	          AND vus.user_id = %s
	          AND vus.watched_at IS NOT NULL
	      )
	    )
	  )`, b.phBool(opts.UnwatchedOnly), b.phText(opts.UserID), b.phText(opts.UserID))
	}
	return fmt.Sprintf(`
	  AND (
	    %s = 0
	    OR (
	      %s <> ''
	      AND status = 'DONE'
	      AND NOT EXISTS (
	        SELECT 1 FROM video_user_states vus
	        WHERE vus.video_id = videos.id
	          AND vus.user_id = %s
	          AND vus.watched_at IS NOT NULL
	      )
	    )
	  )`, b.phBool(opts.UnwatchedOnly), b.phText(opts.UserID), b.phText(opts.UserID))
}

func (b *videoPageBuilder) cursorAndOrderSQL(sort, order string, cursor *VideoListPageCursor, continueWatchingOnly bool) string {
	present := cursor != nil
	var (
		curTime     time.Time
		curSortTime time.Time
		curID       int64
		curNum      *float64
		curInt      *int64
		curText     *string
	)
	if present {
		curTime, curID = cursor.StartDownloadAt, cursor.ID
		curNum, curInt, curText = cursor.SortNumber, cursor.SortInt, cursor.SortText
		curSortTime = curTime
		if cursor.SortTime != nil {
			curSortTime = *cursor.SortTime
		}
	}
	t := func() string { return b.phTime(curTime, present) }
	sortTime := func() string { return b.phTime(curSortTime, present) }
	id := func() string { return b.phID(curID) }
	text := func() string { return b.phTextPtr(curText) }
	num := func() string { return b.phFloatPtr(curNum) }
	bigint := func() string { return b.phIntPtr(curInt) }
	historyWhen := "COALESCE(deleted_at, downloaded_at, start_download_at)"
	// An archive sorts by the date its stream aired; a live recording aired
	// when it was recorded.
	broadcastWhen := "COALESCE(broadcast_at, start_download_at)"

	if sort == "last_watched" {
		op, direction := "<", "DESC"
		if order == "asc" {
			op, direction = ">", "ASC"
		}
		progress := "progress.last_progress_at_ms"
		idColumn := "videos.id"
		if continueWatchingOnly {
			idColumn = "progress.video_id"
		}
		return fmt.Sprintf(`
  AND (
    %s IS NULL
    OR (%s IS NULL AND %s IS NULL AND id %s %s)
    OR (%s IS NOT NULL AND (%s IS NULL OR %s %s %s OR (%s = %s AND id %s %s)))
  )
ORDER BY progress.last_progress_at_ms %s NULLS LAST, %s %s`,
			t(), bigint(), progress, op, id(), bigint(), progress, progress, op, bigint(), progress, bigint(), op, id(), direction, idColumn, direction)
	}

	switch sort + ":" + order {
	case "created_at:asc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR start_download_at > %s OR (start_download_at = %s AND id > %s))
ORDER BY start_download_at ASC, id ASC`, t(), t(), t(), id())
	case "history_when:asc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR %s > %s OR (%s = %s AND id > %s))
ORDER BY %s ASC, id ASC`, sortTime(), historyWhen, sortTime(), historyWhen, sortTime(), id(), historyWhen)
	case "history_when:desc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR %s < %s OR (%s = %s AND id < %s))
ORDER BY %s DESC, id DESC`, sortTime(), historyWhen, sortTime(), historyWhen, sortTime(), id(), historyWhen)
	case "broadcast_at:asc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR %s > %s OR (%s = %s AND id > %s))
ORDER BY %s ASC, id ASC`, sortTime(), broadcastWhen, sortTime(), broadcastWhen, sortTime(), id(), broadcastWhen)
	case "broadcast_at:desc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR %s < %s OR (%s = %s AND id < %s))
ORDER BY %s DESC, id DESC`, sortTime(), broadcastWhen, sortTime(), broadcastWhen, sortTime(), id(), broadcastWhen)
	case "channel:asc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR display_name > %s
    OR (display_name = %s AND (start_download_at < %s OR (start_download_at = %s AND id > %s))))
ORDER BY display_name ASC, start_download_at DESC, id ASC`, text(), text(), text(), t(), t(), id())
	case "channel:desc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR display_name < %s
    OR (display_name = %s AND (start_download_at < %s OR (start_download_at = %s AND id < %s))))
ORDER BY display_name DESC, start_download_at DESC, id DESC`, text(), text(), text(), t(), t(), id())
	case "duration:asc":
		return fmt.Sprintf(`
  AND (
    %s IS NULL
    OR (%s IS NULL AND duration_seconds IS NULL AND (start_download_at < %s OR (start_download_at = %s AND id > %s)))
    OR (%s IS NOT NULL AND (duration_seconds IS NULL OR duration_seconds > %s OR (duration_seconds = %s AND (start_download_at < %s OR (start_download_at = %s AND id > %s)))))
  )
ORDER BY duration_seconds ASC NULLS LAST, start_download_at DESC, id ASC`, t(), num(), t(), t(), id(), num(), num(), num(), t(), t(), id())
	case "duration:desc":
		return fmt.Sprintf(`
  AND (
    %s IS NULL
    OR (%s IS NULL AND duration_seconds IS NULL AND (start_download_at < %s OR (start_download_at = %s AND id < %s)))
    OR (%s IS NOT NULL AND (duration_seconds IS NULL OR duration_seconds < %s OR (duration_seconds = %s AND (start_download_at < %s OR (start_download_at = %s AND id < %s)))))
  )
ORDER BY duration_seconds DESC NULLS LAST, start_download_at DESC, id DESC`, t(), num(), t(), t(), id(), num(), num(), num(), t(), t(), id())
	case "size:asc":
		return fmt.Sprintf(`
  AND (
    %s IS NULL
    OR (%s IS NULL AND size_bytes IS NULL AND (start_download_at < %s OR (start_download_at = %s AND id > %s)))
    OR (%s IS NOT NULL AND (size_bytes IS NULL OR size_bytes > %s OR (size_bytes = %s AND (start_download_at < %s OR (start_download_at = %s AND id > %s)))))
  )
ORDER BY size_bytes ASC NULLS LAST, start_download_at DESC, id ASC`, t(), bigint(), t(), t(), id(), bigint(), bigint(), bigint(), t(), t(), id())
	case "size:desc":
		return fmt.Sprintf(`
  AND (
    %s IS NULL
    OR (%s IS NULL AND size_bytes IS NULL AND (start_download_at < %s OR (start_download_at = %s AND id < %s)))
    OR (%s IS NOT NULL AND (size_bytes IS NULL OR size_bytes < %s OR (size_bytes = %s AND (start_download_at < %s OR (start_download_at = %s AND id < %s)))))
  )
ORDER BY size_bytes DESC NULLS LAST, start_download_at DESC, id DESC`, t(), bigint(), t(), t(), id(), bigint(), bigint(), bigint(), t(), t(), id())
	default: // created_at:desc
		return fmt.Sprintf(`
  AND (%s IS NULL OR start_download_at < %s OR (start_download_at = %s AND id < %s))
ORDER BY start_download_at DESC, id DESC`, t(), t(), t(), id())
	}
}
