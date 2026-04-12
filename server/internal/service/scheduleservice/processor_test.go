package scheduleservice

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// TestHighestQuality_PicksHighRankAndBreaksTiesByID pins the winner-
// selection rule from .docs/spec/eventsub.md: on a stream.online match
// the processor must trigger ONE download at the highest-quality matching
// schedule's quality. Without this, two matching schedules race on the
// downloader's busy-check — whichever Start() call wins decides the
// quality, and if the lower-quality one wins the VOD gets recorded at
// the wrong setting.
//
// The ID-based tie-break keeps the choice deterministic across retries
// of the same event.
func TestHighestQuality_PicksHighRankAndBreaksTiesByID(t *testing.T) {
	cases := []struct {
		name   string
		input  []*repository.DownloadSchedule
		wantID int64
	}{
		{
			name: "single",
			input: []*repository.DownloadSchedule{
				{ID: 7, Quality: repository.QualityMedium},
			},
			wantID: 7,
		},
		{
			name: "HIGH beats MEDIUM beats LOW regardless of order",
			input: []*repository.DownloadSchedule{
				{ID: 3, Quality: repository.QualityLow},
				{ID: 1, Quality: repository.QualityHigh},
				{ID: 2, Quality: repository.QualityMedium},
			},
			wantID: 1,
		},
		{
			name: "ties on HIGH break to lower ID",
			input: []*repository.DownloadSchedule{
				{ID: 42, Quality: repository.QualityHigh},
				{ID: 7, Quality: repository.QualityHigh},
				{ID: 100, Quality: repository.QualityHigh},
			},
			wantID: 7,
		},
		{
			name: "unknown quality sorts below known values",
			input: []*repository.DownloadSchedule{
				{ID: 5, Quality: "BOGUS"},
				{ID: 9, Quality: repository.QualityLow},
			},
			wantID: 9,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := highestQuality(tc.input)
			if got.ID != tc.wantID {
				t.Errorf("got ID=%d, want %d (quality=%q)", got.ID, tc.wantID, got.Quality)
			}
		})
	}
}
