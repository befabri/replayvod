package schedule

import "github.com/befabri/replayvod/server/internal/repository"

// StreamSignals captures the stream-side inputs to the matcher. Passing
// these as explicit values (rather than reading a shared struct) keeps the
// match function's contract tight and the unit tests easy: each field
// means exactly one thing, no hidden coupling to Twitch payload shapes.
type StreamSignals struct {
	ViewerCount int64
	// Category and tag IDs are compared against the schedule's junction
	// rows. Empty slices mean "the stream has no categories/tags set"
	// (common during the first few seconds of a broadcast).
	CategoryIDs []string
	TagIDs      []int64
}

// Filters enumerates the precomputed link rows for a schedule, avoiding
// a repeat query in the tight matching loop on the webhook path.
type Filters struct {
	Categories []repository.Category
	Tags       []repository.Tag
}

// Match returns true when a schedule should trigger a download for the
// observed stream signals. It is the heart of the auto-record feature:
// it runs on every stream.online webhook for every active schedule on
// the affected channel, so it must be cheap and allocation-free.
//
// Semantics:
//   - is_disabled schedules never match. Callers typically filter them
//     out upstream via ListActiveSchedulesForBroadcaster, but the
//     defensive check makes this function safe to use on any row.
//   - has_min_viewers=true requires ViewerCount >= min_viewers.
//     stream.online events may arrive with ViewerCount=0 (Twitch
//     delivers immediately); operators handling fast-streamers use
//     has_min_viewers=false to not miss these.
//   - has_categories=true requires the stream's category to be in the
//     schedule's allowlist. Empty category on the stream means no match.
//     AND semantics with has_tags — the stream must satisfy every
//     active filter, matching v1 behavior.
//   - has_tags=true requires the stream's tag set to intersect the
//     schedule's allowlist (at least one tag in common).
func Match(schedule *repository.DownloadSchedule, filters Filters, signals StreamSignals) bool {
	if schedule == nil || schedule.IsDisabled {
		return false
	}

	if schedule.HasMinViewers {
		if schedule.MinViewers == nil {
			// Guarded by a CHECK constraint in the schema; belt-and-suspenders
			// here because the matcher must never crash on webhook delivery.
			return false
		}
		if signals.ViewerCount < *schedule.MinViewers {
			return false
		}
	}

	if schedule.HasCategories {
		if len(filters.Categories) == 0 || len(signals.CategoryIDs) == 0 {
			return false
		}
		if !hasCategoryOverlap(filters.Categories, signals.CategoryIDs) {
			return false
		}
	}

	if schedule.HasTags {
		if len(filters.Tags) == 0 || len(signals.TagIDs) == 0 {
			return false
		}
		if !hasTagOverlap(filters.Tags, signals.TagIDs) {
			return false
		}
	}

	return true
}

func hasCategoryOverlap(allowed []repository.Category, streamIDs []string) bool {
	set := make(map[string]struct{}, len(streamIDs))
	for _, id := range streamIDs {
		set[id] = struct{}{}
	}
	for _, c := range allowed {
		if _, ok := set[c.ID]; ok {
			return true
		}
	}
	return false
}

func hasTagOverlap(allowed []repository.Tag, streamIDs []int64) bool {
	set := make(map[int64]struct{}, len(streamIDs))
	for _, id := range streamIDs {
		set[id] = struct{}{}
	}
	for _, t := range allowed {
		if _, ok := set[t.ID]; ok {
			return true
		}
	}
	return false
}
