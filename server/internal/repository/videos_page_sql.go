package repository

import (
	"fmt"
	"time"
)

// videosPageColumnsSQL is the shared SELECT/FROM/soft-delete prefix for the
// dialect-aware keyset query builder.
const videosPageColumnsSQL = `SELECT
    id, job_id, filename, display_name, status, quality, selected_quality,
    selected_fps, broadcaster_id, stream_id, viewer_count, language,
    duration_seconds, size_bytes, thumbnail, error,
    start_download_at, downloaded_at, deleted_at,
    recording_type, force_h264, title, completion_kind, truncated,
    trigger_schedule_id, retention_source_schedule_id, retention_window_hours
FROM videos
WHERE deleted_at IS NULL`

// VideoPageDialect supplies the placeholder/cast and timestamp-binding pieces
// that differ between Postgres and SQLite.
type VideoPageDialect struct {
	Postgres bool
	// FormatTime converts a cursor timestamp to the engine's bind value:
	// Postgres binds time.Time directly; SQLite binds the adapter's text form.
	FormatTime func(time.Time) any
}

// videoPageBuilder appends bind args as it renders placeholders. Postgres gets
// distinct $N placeholders; SQLite gets positional ? placeholders.
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

	query := videosPageColumnsSQL +
		fmt.Sprintf("\n  AND (%s = '' OR status = %s)", b.phText(opts.Status), b.phText(opts.Status)) +
		b.filtersSQL(opts) +
		b.cursorAndOrderSQL(sort, order, cursor) +
		fmt.Sprintf("\nLIMIT %s", b.phID(limit))
	return query, b.args
}

func (b *videoPageBuilder) filtersSQL(opts ListVideosOpts) string {
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

	return quality + broadcaster + language + durationMin + durationMax + sizeMin + sizeMax + window + incomplete
}

func (b *videoPageBuilder) cursorAndOrderSQL(sort, order string, cursor *VideoListPageCursor) string {
	present := cursor != nil
	var (
		curTime time.Time
		curID   int64
		curNum  *float64
		curInt  *int64
		curText *string
	)
	if present {
		curTime, curID = cursor.StartDownloadAt, cursor.ID
		curNum, curInt, curText = cursor.SortNumber, cursor.SortInt, cursor.SortText
	}
	t := func() string { return b.phTime(curTime, present) }
	id := func() string { return b.phID(curID) }
	text := func() string { return b.phTextPtr(curText) }
	num := func() string { return b.phFloatPtr(curNum) }
	bigint := func() string { return b.phIntPtr(curInt) }

	switch sort + ":" + order {
	case "created_at:asc":
		return fmt.Sprintf(`
  AND (%s IS NULL OR start_download_at > %s OR (start_download_at = %s AND id > %s))
ORDER BY start_download_at ASC, id ASC`, t(), t(), t(), id())
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
