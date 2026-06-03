package repository

// Pure page/cursor helpers shared by every Repository adapter. They operate only
// on repository types (no SQL, no dialect), so a single copy keeps the keyset
// pagination semantics — over-fetch-by-one, cursor derivation, sort allowlist —
// identical across backends instead of drifting between hand-mirrored adapters.

// ToChannelPage trims an over-fetched channel slice to limit and derives the
// next cursor from the last kept row. limit <= 0 yields an empty page.
func ToChannelPage(items []Channel, limit int) *ChannelPage {
	if limit <= 0 {
		return &ChannelPage{Items: []Channel{}}
	}
	page := &ChannelPage{Items: items}
	if len(items) <= limit {
		return page
	}
	page.Items = items[:limit]
	next := page.Items[len(page.Items)-1]
	page.NextCursor = &ChannelPageCursor{
		BroadcasterName: next.BroadcasterName,
		BroadcasterID:   next.BroadcasterID,
	}
	return page
}

// NormalizeVideoListSort clamps opts.Sort/Order to the supported allowlist,
// defaulting to created_at/desc. It is the single source of truth for which
// video-list sorts exist.
func NormalizeVideoListSort(opts ListVideosOpts) (string, string) {
	sort := opts.Sort
	order := opts.Order
	switch sort {
	case "created_at", "duration", "size", "channel":
	default:
		return "created_at", "desc"
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	return sort, order
}

// ListVideosPageQueryLimit is the over-fetch-by-one query limit: fetch limit+1
// rows so the presence of an extra row signals there is a next page.
func ListVideosPageQueryLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	return limit + 1
}

// ToVideoListPage trims an over-fetched video slice to opts.Limit and derives
// the sort-aware next cursor.
func ToVideoListPage(items []Video, opts ListVideosOpts) *VideoListPage {
	if opts.Limit <= 0 {
		return &VideoListPage{Items: []Video{}}
	}
	page := &VideoListPage{Items: items}
	if len(items) <= opts.Limit {
		return page
	}
	page.Items = items[:opts.Limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor = VideoListCursorFromVideo(&last, opts)
	return page
}

// VideoListCursorFromVideo builds the keyset cursor for v under the given sort,
// carrying the sort-column value alongside the (start_download_at, id) tie-break.
func VideoListCursorFromVideo(v *Video, opts ListVideosOpts) *VideoListPageCursor {
	if v == nil {
		return nil
	}
	cursor := &VideoListPageCursor{StartDownloadAt: v.StartDownloadAt, ID: v.ID}
	sort, _ := NormalizeVideoListSort(opts)
	switch sort {
	case "duration":
		cursor.SortNumber = v.DurationSeconds
	case "size":
		cursor.SortInt = v.SizeBytes
	case "channel":
		cursor.SortText = &v.DisplayName
	}
	return cursor
}

// ToVideoPage trims an over-fetched video slice to limit and derives the
// (start_download_at, id) next cursor used by the broadcaster/category lists.
func ToVideoPage(items []Video, limit int) *VideoPage {
	if limit <= 0 {
		return &VideoPage{Items: []Video{}}
	}
	page := &VideoPage{Items: items}
	if len(items) <= limit {
		return page
	}
	page.Items = items[:limit]
	next := page.Items[len(page.Items)-1]
	page.NextCursor = &VideoPageCursor{StartDownloadAt: next.StartDownloadAt, ID: next.ID}
	return page
}
