package scheduleservice

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func int64Ptr(v int64) *int64 { return &v }

// TestMatchSchedule_TableDriven covers every branch of the matcher's
// decision tree. Each case is named after the behavior under test so a
// regression in the matcher surfaces in the test name, not just a boolean
// mismatch.
func TestMatchSchedule_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		schedule repository.DownloadSchedule
		filters  ScheduleFilters
		signals  StreamSignals
		want     bool
	}{
		{
			name:     "default schedule with no filters matches",
			schedule: repository.DownloadSchedule{},
			want:     true,
		},
		{
			name:     "disabled schedule never matches",
			schedule: repository.DownloadSchedule{IsDisabled: true},
			want:     false,
		},
		{
			name:     "min viewers met",
			schedule: repository.DownloadSchedule{HasMinViewers: true, MinViewers: int64Ptr(100)},
			signals:  StreamSignals{ViewerCount: 150},
			want:     true,
		},
		{
			name:     "min viewers exact threshold",
			schedule: repository.DownloadSchedule{HasMinViewers: true, MinViewers: int64Ptr(100)},
			signals:  StreamSignals{ViewerCount: 100},
			want:     true,
		},
		{
			name:     "min viewers not met",
			schedule: repository.DownloadSchedule{HasMinViewers: true, MinViewers: int64Ptr(100)},
			signals:  StreamSignals{ViewerCount: 99},
			want:     false,
		},
		{
			name:     "min viewers enabled with nil pointer rejects defensively",
			schedule: repository.DownloadSchedule{HasMinViewers: true, MinViewers: nil},
			signals:  StreamSignals{ViewerCount: 1_000_000},
			want:     false,
		},
		{
			name:     "category overlap",
			schedule: repository.DownloadSchedule{HasCategories: true},
			filters:  ScheduleFilters{Categories: []repository.Category{{ID: "509658"}, {ID: "33214"}}},
			signals:  StreamSignals{CategoryIDs: []string{"509658"}},
			want:     true,
		},
		{
			name:     "category mismatch",
			schedule: repository.DownloadSchedule{HasCategories: true},
			filters:  ScheduleFilters{Categories: []repository.Category{{ID: "509658"}}},
			signals:  StreamSignals{CategoryIDs: []string{"33214"}},
			want:     false,
		},
		{
			name:     "category filter enabled with empty filter list fails",
			schedule: repository.DownloadSchedule{HasCategories: true},
			filters:  ScheduleFilters{Categories: nil},
			signals:  StreamSignals{CategoryIDs: []string{"509658"}},
			want:     false,
		},
		{
			name:     "category filter enabled with empty stream category fails",
			schedule: repository.DownloadSchedule{HasCategories: true},
			filters:  ScheduleFilters{Categories: []repository.Category{{ID: "509658"}}},
			signals:  StreamSignals{},
			want:     false,
		},
		{
			name:     "tag overlap",
			schedule: repository.DownloadSchedule{HasTags: true},
			filters:  ScheduleFilters{Tags: []repository.Tag{{ID: 1}, {ID: 2}}},
			signals:  StreamSignals{TagIDs: []int64{2, 3}},
			want:     true,
		},
		{
			name:     "tag mismatch",
			schedule: repository.DownloadSchedule{HasTags: true},
			filters:  ScheduleFilters{Tags: []repository.Tag{{ID: 1}}},
			signals:  StreamSignals{TagIDs: []int64{2}},
			want:     false,
		},
		{
			name: "all filters enabled and satisfied",
			schedule: repository.DownloadSchedule{
				HasMinViewers: true, MinViewers: int64Ptr(10),
				HasCategories: true,
				HasTags:       true,
			},
			filters: ScheduleFilters{
				Categories: []repository.Category{{ID: "cat-1"}},
				Tags:       []repository.Tag{{ID: 7}},
			},
			signals: StreamSignals{
				ViewerCount: 10,
				CategoryIDs: []string{"cat-1"},
				TagIDs:      []int64{7, 42},
			},
			want: true,
		},
		{
			name: "all filters enabled, viewers fail short-circuits",
			schedule: repository.DownloadSchedule{
				HasMinViewers: true, MinViewers: int64Ptr(100),
				HasCategories: true,
				HasTags:       true,
			},
			filters: ScheduleFilters{
				Categories: []repository.Category{{ID: "cat-1"}},
				Tags:       []repository.Tag{{ID: 7}},
			},
			signals: StreamSignals{
				ViewerCount: 5,
				CategoryIDs: []string{"cat-1"},
				TagIDs:      []int64{7},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchSchedule(&tc.schedule, tc.filters, tc.signals)
			if got != tc.want {
				t.Errorf("MatchSchedule() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMatchSchedule_NilSchedule_NeverMatches guards against a nil-pointer
// panic on the webhook hot path. The dispatcher loops over
// ListActiveSchedulesForBroadcaster results; a DB glitch or in-flight
// delete could plausibly surface a nil entry, and we'd rather skip it
// than crash the webhook handler.
func TestMatchSchedule_NilSchedule_NeverMatches(t *testing.T) {
	if MatchSchedule(nil, ScheduleFilters{}, StreamSignals{}) {
		t.Fatal("nil schedule must never match")
	}
}
