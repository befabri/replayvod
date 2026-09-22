package repository

import (
	"fmt"
	"time"
)

// MaxBatchSize bounds the rows returned by background scan and sample queries.
// It is a workload policy, not a database limit or a default batch size.
const MaxBatchSize = 1000

// BatchSize is an immutable, bounded row count. Construct it with NewBatchSize;
// its zero value is invalid and repositories reject it before querying.
type BatchSize struct {
	limit int
}

func NewBatchSize(limit int) (BatchSize, error) {
	size := BatchSize{limit: limit}
	if err := size.Validate(); err != nil {
		return BatchSize{}, err
	}
	return size, nil
}

func (s BatchSize) Limit() int { return s.limit }

func (s BatchSize) Validate() error {
	if s.limit < 1 || s.limit > MaxBatchSize {
		return fmt.Errorf("invalid batch size %d: require between 1 and %d", s.limit, MaxBatchSize)
	}
	return nil
}

// BatchPage is an immutable keyset page for background scans. AfterID zero
// starts at the beginning. Construct it with NewBatchPage; the zero value is
// invalid. Repository methods validate it even when a constructor error was
// ignored, so an uninitialized page can never reach SQL.
type BatchPage struct {
	afterID int64
	size    BatchSize
}

func NewBatchPage(afterID int64, limit int) (BatchPage, error) {
	page := BatchPage{afterID: afterID, size: BatchSize{limit: limit}}
	if err := page.Validate(); err != nil {
		return BatchPage{}, err
	}
	return page, nil
}

func (p BatchPage) AfterID() int64 { return p.afterID }
func (p BatchPage) Limit() int     { return p.size.Limit() }

func (p BatchPage) Validate() error {
	if p.afterID < 0 {
		return fmt.Errorf("invalid batch cursor %d: require a non-negative ID", p.afterID)
	}
	return p.size.Validate()
}

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

// NormalizeCategoryPageSort clamps the category browse sort allowlist to the
// current default: alphabetical by category name.
func NormalizeCategoryPageSort(sort string) string {
	switch sort {
	case "latest_video_desc", "video_count_desc":
		return sort
	default:
		return "name_asc"
	}
}

// ToCategoryPage trims an over-fetched category slice to limit and derives the
// next cursor from the last kept row.
func ToCategoryPage(items []CategoryPageItem, limit int, sort string) *CategoryPage {
	if limit <= 0 {
		return &CategoryPage{Items: []Category{}}
	}
	page := &CategoryPage{Items: make([]Category, 0, min(len(items), limit))}
	kept := items
	if len(items) > limit {
		kept = items[:limit]
	}
	for _, item := range kept {
		page.Items = append(page.Items, item.Category)
	}
	if len(items) <= limit {
		return page
	}
	last := kept[len(kept)-1]
	cursor := &CategoryPageCursor{
		Name: last.Category.Name,
		ID:   last.Category.ID,
	}
	switch NormalizeCategoryPageSort(sort) {
	case "latest_video_desc":
		latest := last.LatestVideoAt
		cursor.LatestVideoAt = &latest
	case "video_count_desc":
		cursor.VideoCount = last.VideoCount
	}
	page.NextCursor = cursor
	return page
}

// NormalizeVideoListSort returns the supported sort and order, defaulting to
// created_at descending for an unknown sort and descending for an unknown order.
func NormalizeVideoListSort(opts ListVideosOpts) (string, string) {
	sort := opts.Sort
	order := opts.Order
	switch sort {
	case "created_at", "duration", "size", "channel", "history_when", "broadcast_at", "last_watched":
	default:
		return "created_at", "desc"
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	return sort, order
}

// ListVideosPageQueryLimit includes one extra row to detect a following page.
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

// VideoListCursorFromVideo returns the continuation cursor for v and opts.Sort.
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
	case "last_watched":
		cursor.SortInt = v.LastProgressAtMs
	case "channel":
		cursor.SortText = &v.DisplayName
	case "history_when":
		sortTime := VideoHistoryWhen(v)
		cursor.SortTime = &sortTime
	case "broadcast_at":
		sortTime := VideoBroadcastWhen(v)
		cursor.SortTime = &sortTime
	}
	return cursor
}

// VideoHistoryWhen returns the shared history display and pagination timestamp.
func VideoHistoryWhen(v *Video) time.Time {
	if v == nil {
		return time.Time{}
	}
	if v.DeletedAt != nil {
		return *v.DeletedAt
	}
	if v.DownloadedAt != nil {
		return *v.DownloadedAt
	}
	return v.StartDownloadAt
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

// VideoBroadcastWhen returns the shared air-date display and pagination timestamp.
func VideoBroadcastWhen(v *Video) time.Time {
	if v.BroadcastAt != nil {
		return *v.BroadcastAt
	}
	return v.StartDownloadAt
}
