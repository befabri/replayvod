package repository

import "testing"

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
