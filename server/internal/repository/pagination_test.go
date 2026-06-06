package repository

import (
	"testing"
	"time"
)

func TestNormalizeVideoListSort(t *testing.T) {
	cases := []struct {
		name      string
		sort      string
		order     string
		wantSort  string
		wantOrder string
	}{
		{"valid duration asc", "duration", "asc", "duration", "asc"},
		{"valid size desc", "size", "desc", "size", "desc"},
		{"valid channel asc", "channel", "asc", "channel", "asc"},
		{"valid history_when desc", "history_when", "desc", "history_when", "desc"},
		{"valid created_at desc", "created_at", "desc", "created_at", "desc"},
		{"unknown column collapses to created_at desc", "garbage", "asc", "created_at", "desc"},
		{"empty collapses to created_at desc", "", "", "created_at", "desc"},
		{"valid column, bad order clamps to desc", "duration", "sideways", "duration", "desc"},
		{"valid column, empty order clamps to desc", "size", "", "size", "desc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sort, order := NormalizeVideoListSort(ListVideosOpts{Sort: tc.sort, Order: tc.order})
			if sort != tc.wantSort || order != tc.wantOrder {
				t.Fatalf("NormalizeVideoListSort(%q,%q) = (%q,%q), want (%q,%q)",
					tc.sort, tc.order, sort, order, tc.wantSort, tc.wantOrder)
			}
		})
	}
}

func TestSortKey(t *testing.T) {
	cases := []struct {
		name  string
		sort  string
		order string
		want  string
	}{
		{"valid duration asc", "duration", "asc", "duration-asc"},
		{"valid size desc", "size", "desc", "size-desc"},
		{"valid channel asc", "channel", "asc", "channel-asc"},
		{"valid history_when desc", "history_when", "desc", "history_when-desc"},
		{"unknown column → default token", "garbage", "asc", "created_at-desc"},
		{"empty → default token (was empty string)", "", "", "created_at-desc"},
		{"valid column, bad order → desc", "duration", "sideways", "duration-desc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (ListVideosOpts{Sort: tc.sort, Order: tc.order}).SortKey(); got != tc.want {
				t.Fatalf("SortKey(%q,%q) = %q, want %q", tc.sort, tc.order, got, tc.want)
			}
		})
	}
}

func TestVideoListCursorFromVideo_HistoryWhen(t *testing.T) {
	started := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	downloaded := started.Add(2 * time.Hour)
	deleted := started.Add(24 * time.Hour)
	v := &Video{ID: 9, StartDownloadAt: started, DownloadedAt: &downloaded, DeletedAt: &deleted}

	cursor := VideoListCursorFromVideo(v, ListVideosOpts{Sort: "history_when", Order: "desc"})
	if cursor.SortTime == nil || !cursor.SortTime.Equal(deleted) {
		t.Fatalf("SortTime = %v, want deleted_at %v", cursor.SortTime, deleted)
	}
	if !cursor.StartDownloadAt.Equal(started) {
		t.Fatalf("StartDownloadAt = %v, want %v", cursor.StartDownloadAt, started)
	}
}
