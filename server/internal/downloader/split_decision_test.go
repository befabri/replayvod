package downloader

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

// TestShouldForceSplitOnRestartGap: the threshold check scales
// correctly with targetDuration (Twitch typically uses 2s or 6s,
// not the test fixture's 1s).
func TestShouldForceSplitOnRestartGap(t *testing.T) {
	withContent := &ResumeState{PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 109}
	noContent := &ResumeState{}

	cases := []struct {
		name      string
		from, to  int64
		td        time.Duration
		threshold int
		resume    *ResumeState
		want      bool
	}{
		{"1s td, 8 segs lost, 2s threshold — over", 110, 117, time.Second, 2, withContent, true},
		{"1s td, 2 segs lost, 2s threshold — at boundary, false", 110, 111, time.Second, 2, withContent, false},
		{"2s td, 2 segs lost, 2s threshold — 4s>2s over", 110, 111, 2 * time.Second, 2, withContent, true},
		{"2s td, 1 seg lost, 2s threshold — 2s at boundary, false", 110, 110, 2 * time.Second, 2, withContent, false},
		{"6s td, 1 seg lost, 2s threshold — 6s>2s over", 110, 110, 6 * time.Second, 2, withContent, true},
		{"6s td, 5 segs lost, 60s threshold — 30s<60s under", 110, 114, 6 * time.Second, 60, withContent, false},
		{"1s td, 60s lost, 120s default threshold — under", 110, 169, time.Second, 120, withContent, false},
		{"6s td, 25 segs lost, 120s default threshold — 150s>120s over", 110, 134, 6 * time.Second, 120, withContent, true},
		{"threshold 0 disabled even with huge gap", 110, 999, time.Second, 0, withContent, false},
		{"threshold negative same as 0", 110, 999, time.Second, -1, withContent, false},
		{"over threshold but no part content — no split (doom-loop guard)", 110, 999, time.Second, 2, noContent, false},
		{"zero td, any seg count — never triggers", 110, 117, 0, 2, withContent, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldForceSplitOnRestartGap(tc.from, tc.to, tc.td, tc.threshold, tc.resume)
			if got != tc.want {
				t.Errorf("shouldForceSplitOnRestartGap = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPartEndMediaSeq(t *testing.T) {
	cases := []struct {
		name      string
		hlsResult *hls.JobResult
		frontier  int64
		want      int64
	}{
		{
			name:      "normal completion: hls result greater",
			hlsResult: &hls.JobResult{LastMediaSeq: 200},
			frontier:  100,
			want:      200,
		},
		{
			name:      "restart-gap split: frontier greater (hls cancelled before commits)",
			hlsResult: &hls.JobResult{LastMediaSeq: 0},
			frontier:  150,
			want:      150,
		},
		{
			name:      "exactly equal",
			hlsResult: &hls.JobResult{LastMediaSeq: 175},
			frontier:  175,
			want:      175,
		},
		{
			name:      "nil hlsResult: frontier wins",
			hlsResult: nil,
			frontier:  120,
			want:      120,
		},
		{
			name:      "nil hlsResult, fresh part (frontier=0)",
			hlsResult: nil,
			frontier:  0,
			want:      0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := partEndMediaSeq(tc.hlsResult, tc.frontier); got != tc.want {
				t.Errorf("partEndMediaSeq = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestHasPartContent: the PartStart > 0 guard prevents a doom loop
// after BeginNewPart, where PartStart=frontier=0 would falsely
// report content via the >= comparison.
func TestHasPartContent(t *testing.T) {
	cases := []struct {
		name      string
		hlsResult *hls.JobResult
		resume    *ResumeState
		want      bool
	}{
		{
			name:      "current attempt fetched segments — content",
			hlsResult: &hls.JobResult{SegmentsDone: 5},
			resume:    &ResumeState{},
			want:      true,
		},
		{
			name:      "nil hlsResult, prior attempt anchored + advanced — content",
			hlsResult: nil,
			resume:    &ResumeState{PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 105},
			want:      true,
		},
		{
			name:      "nil hlsResult, prior anchored at exact start — content (one commit)",
			hlsResult: nil,
			resume:    &ResumeState{PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 100},
			want:      true,
		},
		{
			name:      "nil hlsResult, prior anchored but no commits yet — no content",
			hlsResult: nil,
			resume:    &ResumeState{PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 99},
			want:      false,
		},
		{
			name:      "post-BeginNewPart fresh state (PartStart=0, frontier=0) — NO content (doom-loop guard)",
			hlsResult: nil,
			resume:    &ResumeState{PartStartMediaSequence: 0, AccountedFrontierMediaSeq: 0},
			want:      false,
		},
		{
			name:      "current attempt zero done + post-BeginNewPart — NO content (combined doom-loop guard)",
			hlsResult: &hls.JobResult{SegmentsDone: 0},
			resume:    &ResumeState{PartStartMediaSequence: 0, AccountedFrontierMediaSeq: 0},
			want:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasPartContent(tc.hlsResult, tc.resume); got != tc.want {
				t.Errorf("hasPartContent = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsSplitSignal pins the classification rule the outer part loop
// uses to decide "finalize this part and re-enter for a new one"
// vs. "hard-fail the job." Both signal shapes per spec §"Variant
// loss mid-stream" must classify as split; everything else must not.
//
// Wrapped errors must still match — the loop's caller wraps via
// fmt.Errorf("…: %w", err) before checking. A predicate that only
// matches bare sentinels would silently miss the real cases.
func TestIsSplitSignal(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "playlist gone (bare sentinel)",
			err:  hls.ErrPlaylistGone,
			want: true,
		},
		{
			name: "playlist gone (wrapped)",
			err:  fmt.Errorf("hls run: %w", hls.ErrPlaylistGone),
			want: true,
		},
		{
			name: "variant changed (bare sentinel)",
			err:  ErrVariantChanged,
			want: true,
		},
		{
			name: "variant changed (wrapped quality)",
			err:  fmt.Errorf("%w: quality %q → %q", ErrVariantChanged, "1080", "720"),
			want: true,
		},
		{
			name: "restart gap exceeded (bare sentinel)",
			err:  ErrRestartGapExceeded,
			want: true,
		},
		{
			name: "restart gap exceeded (wrapped)",
			err:  fmt.Errorf("%w: hls run cancelled at restart-gap boundary", ErrRestartGapExceeded),
			want: true,
		},
		{
			name: "playlist auth (must NOT split — auth refresh path)",
			err:  hls.ErrPlaylistAuth,
			want: false,
		},
		{
			name: "permanent auth (must NOT split — entitlement)",
			err:  hls.ErrPlaylistAuthPermanent,
			want: false,
		},
		{
			name: "transport error (must NOT split — gap policy)",
			err:  errors.New("hls poller: status 500: server error"),
			want: false,
		},
		{
			name: "user cancel (must NOT split — terminal)",
			err:  ErrCancelled,
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isSplitSignal(tc.err)
			if got != tc.want {
				t.Errorf("isSplitSignal(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
