package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

// TestShouldForceSplitOnRestartGap: the threshold check scales
// correctly with targetDuration (Twitch typically uses 2s or 6s,
// not the test fixture's 1s).
func TestShouldForceSplitOnRestartGap(t *testing.T) {
	withContent := &ResumeState{PartStarted: true, PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 109}
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

// TestThresholdLimitReached pins the size/duration ceiling decision used by
// NoteCommittedSegmentUntilThreshold / advanceUntilThreshold. Either dimension
// triggers independently; 0/negative disables that dimension; and the boundary
// is "at or over."
func TestThresholdLimitReached(t *testing.T) {
	cases := []struct {
		name     string
		bytes    int64
		seconds  float64
		maxBytes int64
		maxSecs  int
		want     bool
	}{
		{"both disabled (0,0) — never splits even when huge", 1 << 40, 99999, 0, 0, false},
		{"both negative — same as disabled", 1 << 40, 99999, -1, -1, false},
		{"bytes under ceiling", 500, 0, 1000, 0, false},
		{"bytes exactly at ceiling — split (at-or-over)", 1000, 0, 1000, 0, true},
		{"bytes over ceiling — split", 1500, 0, 1000, 0, true},
		{"duration under ceiling", 0, 2.0, 0, 3, false},
		{"duration exactly at ceiling — split (at-or-over)", 0, 3.0, 0, 3, true},
		{"duration over ceiling — split", 0, 4.5, 0, 3, true},
		{"only bytes enabled, duration huge but maxSecs=0 — bytes under, no split", 500, 99999, 1000, 0, false},
		{"only duration enabled, bytes huge but maxBytes=0 — duration under, no split", 1 << 40, 1.0, 0, 3, false},
		{"either dimension crosses — bytes over while duration under", 2000, 1.0, 1000, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := thresholdLimitReached(tc.bytes, tc.seconds, tc.maxBytes, tc.maxSecs)
			if got != tc.want {
				t.Errorf("thresholdLimitReached = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPartEndMediaSeq(t *testing.T) {
	cases := []struct {
		name      string
		hlsResult *hls.JobResult
		resume    *ResumeState
		want      int64
	}{
		{
			name:      "normal completion: hls result greater",
			hlsResult: &hls.JobResult{LastMediaSeq: 200},
			resume:    &ResumeState{AccountedFrontierMediaSeq: 100},
			want:      200,
		},
		{
			name:      "restart-gap split: frontier greater (hls cancelled before commits)",
			hlsResult: &hls.JobResult{LastMediaSeq: 0},
			resume:    &ResumeState{AccountedFrontierMediaSeq: 150},
			want:      150,
		},
		{
			name:      "exactly equal",
			hlsResult: &hls.JobResult{LastMediaSeq: 175},
			resume:    &ResumeState{AccountedFrontierMediaSeq: 175},
			want:      175,
		},
		{
			name:      "nil hlsResult: frontier wins",
			hlsResult: nil,
			resume:    &ResumeState{AccountedFrontierMediaSeq: 120},
			want:      120,
		},
		{
			name:      "nil hlsResult, fresh part (frontier=0)",
			hlsResult: nil,
			resume:    &ResumeState{},
			want:      0,
		},
		{
			name:      "threshold split uses stored boundary over later drained result",
			hlsResult: &hls.JobResult{LastMediaSeq: 104},
			resume: &ResumeState{
				AccountedFrontierMediaSeq:    100,
				PendingThresholdSplit:        true,
				PendingSplitBoundaryMediaSeq: 100,
				PendingSplitBoundarySet:      true,
			},
			want: 100,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := partEndMediaSeq(tc.hlsResult, tc.resume); got != tc.want {
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
			resume:    &ResumeState{PartStarted: true, PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 105},
			want:      true,
		},
		{
			name:      "nil hlsResult, prior anchored at exact start — content (one commit)",
			hlsResult: nil,
			resume:    &ResumeState{PartStarted: true, PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 100},
			want:      true,
		},
		{
			name:      "media sequence zero anchored + advanced — content",
			hlsResult: nil,
			resume:    &ResumeState{PartStarted: true, PartStartMediaSequence: 0, AccountedFrontierMediaSeq: 0},
			want:      true,
		},
		{
			name:      "nil hlsResult, prior anchored but no commits yet — no content",
			hlsResult: nil,
			resume:    &ResumeState{PartStarted: true, PartStartMediaSequence: 100, AccountedFrontierMediaSeq: 99},
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

func TestShouldFinalizeEmptyContinuation(t *testing.T) {
	empty := &ResumeState{}
	if !shouldFinalizeEmptyContinuation(1, &hls.JobResult{EndList: true}, empty) {
		t.Fatal("empty continuation with ENDLIST should finalize prior parts")
	}
	if shouldFinalizeEmptyContinuation(1, &hls.JobResult{EndList: false}, empty) {
		t.Fatal("empty continuation without ENDLIST must not mark success")
	}
	if shouldFinalizeEmptyContinuation(0, &hls.JobResult{EndList: true}, empty) {
		t.Fatal("empty first part must not be treated as successful completion")
	}
	if shouldFinalizeEmptyContinuation(1, &hls.JobResult{EndList: true, SegmentsDone: 1}, empty) {
		t.Fatal("continuation with committed media must be remuxed, not finalized as empty")
	}
	// P1: a resumed part whose media was committed BEFORE the crash (this
	// run sees ENDLIST with SegmentsDone==0) must NOT be finalized as empty
	// — its durable frontier holds real, non-gap segments.
	resumedWithMedia := &ResumeState{
		PartStarted:               true,
		PartStartMediaSequence:    100,
		AccountedFrontierMediaSeq: 104, // 100..104 all committed, no gaps
	}
	if shouldFinalizeEmptyContinuation(1, &hls.JobResult{EndList: true, SegmentsDone: 0}, resumedWithMedia) {
		t.Fatal("resumed continuation with prior on-disk media must be finalized via runPart, not dropped as empty")
	}
}

func TestShouldFinalizeEmptyContinuation_IgnoresResolvedAdOnlyFrontier(t *testing.T) {
	resume := &ResumeState{
		PartStarted:               true,
		PartStartMediaSequence:    100,
		AccountedFrontierMediaSeq: 102,
		Gaps: []Gap{
			{MediaSeq: 100, EndMediaSeq: 100, Reason: GapReasonStitchedAd},
			{MediaSeq: 101, EndMediaSeq: 101, Reason: GapReasonStitchedAd},
			{MediaSeq: 102, EndMediaSeq: 102, Reason: GapReasonStitchedAd},
		},
	}
	hlsResult := &hls.JobResult{
		EndList:        true,
		LastMediaSeq:   102,
		SegmentsDone:   0,
		SegmentsAdGaps: 3,
	}

	if !hasPartContent(hlsResult, resume) {
		t.Fatal("test setup invalid: ad-only gaps should advance the resolved frontier")
	}
	if hasCommittedMedia(hlsResult) {
		t.Fatal("test setup invalid: ad-only continuation must not have committed media")
	}
	if !shouldFinalizeEmptyContinuation(1, hlsResult, resume) {
		t.Fatal("ad-only ENDLIST continuation should finalize prior parts instead of remuxing an empty segment dir")
	}
}

func TestEmptyEndListContinuationResumeShapeSkipsFetch(t *testing.T) {
	resume := &ResumeState{
		Stage:                     StagePrepareInput,
		CurrentPartIndex:          2,
		PartStarted:               true,
		PartStartMediaSequence:    101,
		AccountedFrontierMediaSeq: 100,
		SegmentFormat:             "ts",
		EndListSeen:               true,
	}

	if !shouldSkipSegmentFetch(resume) {
		t.Fatal("empty ENDLIST continuation checkpoint must skip HLS fetch on resume")
	}
	got := synthesizeHLSResultFromResume(resume, hls.SegmentKindTS)
	if !got.EndList || got.SegmentsDone != 0 {
		t.Fatalf("synthesized result = EndList:%v SegmentsDone:%d, want true/0", got.EndList, got.SegmentsDone)
	}
	if !shouldFinalizeEmptyContinuation(1, got, resume) {
		t.Fatal("synthesized empty ENDLIST continuation should finalize already-persisted prior parts")
	}
}

func TestShouldSkipEmptySplitPart_AllowsAdOnlySplitContinuation(t *testing.T) {
	resume := &ResumeState{
		PartStarted:               true,
		PartStartMediaSequence:    100,
		AccountedFrontierMediaSeq: 102,
		PendingSplit:              true,
		Gaps: []Gap{
			{MediaSeq: 100, EndMediaSeq: 100, Reason: GapReasonStitchedAd},
			{MediaSeq: 101, EndMediaSeq: 101, Reason: GapReasonStitchedAd},
			{MediaSeq: 102, EndMediaSeq: 102, Reason: GapReasonStitchedAd},
		},
	}
	hlsResult := &hls.JobResult{
		LastMediaSeq:   102,
		SegmentsDone:   0,
		SegmentsAdGaps: 3,
	}

	if !hasPartContent(hlsResult, resume) {
		t.Fatal("test setup invalid: split guard should see resolved ad-only frontier as content")
	}
	if !shouldSkipEmptySplitPart(1, hlsResult, resume) {
		t.Fatal("ad-only pending split should skip remux for empty interval and open the next part")
	}
	if shouldSkipEmptySplitPart(0, hlsResult, resume) {
		t.Fatal("first part with no committed media must not be skipped as a successful empty split interval")
	}
	resume.PendingSplit = false
	if shouldSkipEmptySplitPart(1, hlsResult, resume) {
		t.Fatal("empty non-split continuation must fail instead of silently opening a new part")
	}
}

func TestShouldAcceptEmptySplitSignal_BoundaryVariantChange(t *testing.T) {
	err := fmt.Errorf("%w: quality 720 -> 480", ErrVariantChanged)
	if !shouldAcceptEmptySplitSignal(1, &hls.JobResult{}, err) {
		t.Fatal("empty continuation variant change should be accepted so the part can re-anchor without remux")
	}
	if shouldAcceptEmptySplitSignal(0, &hls.JobResult{}, err) {
		t.Fatal("empty first-part variant change must not be accepted; that would create a split loop")
	}
	if shouldAcceptEmptySplitSignal(1, &hls.JobResult{SegmentsDone: 1}, err) {
		t.Fatal("variant change after committed media should finalize/remux the current part, not skip it as empty")
	}
	if shouldAcceptEmptySplitSignal(1, &hls.JobResult{}, errors.New("transport failed")) {
		t.Fatal("non-split error on empty continuation must not be accepted")
	}
}

func TestShouldSkipEmptySplitPart_WindowRollBeforeFirstMedia(t *testing.T) {
	resume := &ResumeState{
		PartStarted:               true,
		PartStartMediaSequence:    100,
		AccountedFrontierMediaSeq: 120,
		PendingSplit:              true,
		Gaps: []Gap{{
			MediaSeq:    100,
			EndMediaSeq: 120,
			Reason:      GapReasonRestartWindowRolled,
		}},
	}
	hlsResult := &hls.JobResult{LastMediaSeq: 120}

	if !hasPartContent(hlsResult, resume) {
		t.Fatal("test setup invalid: restart window roll should advance resolved frontier")
	}
	if hasCommittedMedia(hlsResult) {
		t.Fatal("test setup invalid: no media should have committed before the window roll split")
	}
	if !shouldSkipEmptySplitPart(1, hlsResult, resume) {
		t.Fatal("window-roll-only split continuation should open next part without remux/fail")
	}
}

// TestShouldSkipEmptySplitPart_DurableMediaFinalizes pins the run-loop
// routing fix: a split part that holds durable committed media must NOT
// be treated as an empty split even when THIS attempt committed nothing
// new (SegmentsDone==0). Two such shapes: a sealed threshold split whose
// fold sealed a boundary on resume, and any resumed part whose segments
// were captured before the crash. Both finalize through (prune+)runPart;
// without the resume-aware check shouldSkipEmptySplitPart would reanchor
// and abandon the on-disk segments. A genuinely empty interval (frontier
// never advanced past the anchor, or advanced only through gaps) is still
// skipped + reanchored.
func TestShouldSkipEmptySplitPart_DurableMediaFinalizes(t *testing.T) {
	noNewThisRun := &hls.JobResult{LastMediaSeq: 104, SegmentsDone: 0}
	if hasCommittedMedia(noNewThisRun) {
		t.Fatal("test setup invalid: this run must have committed no new media")
	}

	sealed := &ResumeState{
		PartStarted:                  true,
		PartStartMediaSequence:       100,
		AccountedFrontierMediaSeq:    104, // 100..104 committed before the crash
		PendingSplit:                 true,
		PendingThresholdSplit:        true,
		PendingSplitBoundaryMediaSeq: 104,
		PendingSplitBoundarySet:      true,
	}
	if shouldSkipEmptySplitPart(1, noNewThisRun, sealed) {
		t.Fatal("sealed threshold split with durable committed media must finalize, not be skipped")
	}

	// P1: a plain (non-threshold) pending split whose part already holds
	// committed frontier media from before the crash must also finalize.
	resumedWithMedia := &ResumeState{
		PartStarted:               true,
		PartStartMediaSequence:    100,
		AccountedFrontierMediaSeq: 104,
		PendingSplit:              true,
	}
	if shouldSkipEmptySplitPart(1, noNewThisRun, resumedWithMedia) {
		t.Fatal("resumed split part with prior on-disk media must finalize, not be reanchored")
	}

	// A genuinely empty split interval — frontier never advanced past the
	// part anchor, nothing committed anywhere — is still skipped.
	emptyInterval := &ResumeState{
		PartStarted:               true,
		PartStartMediaSequence:    200,
		AccountedFrontierMediaSeq: 199,
		PendingSplit:              true,
	}
	if !shouldSkipEmptySplitPart(1, &hls.JobResult{SegmentsDone: 0}, emptyInterval) {
		t.Fatal("a genuinely empty split interval must still be skipped + reanchored")
	}
}

func TestCaptureHadWindowRollBeforeEmptyContinuationBreak(t *testing.T) {
	resume := &ResumeState{
		Gaps: []Gap{{MediaSeq: 50, EndMediaSeq: 55, Reason: GapReasonRestartWindowRolled}},
	}
	hadWindowRoll := false

	if !captureHadWindowRoll(resume, &hadWindowRoll) {
		t.Fatal("captureHadWindowRoll returned false, want changed")
	}
	if !hadWindowRoll || !resume.HadWindowRoll {
		t.Fatalf("window roll not captured: local=%v resume=%v", hadWindowRoll, resume.HadWindowRoll)
	}
	if captureHadWindowRoll(resume, &hadWindowRoll) {
		t.Fatal("second capture reported changed; want idempotent false")
	}
}

func TestShouldSkipSegmentFetch(t *testing.T) {
	cases := []struct {
		name   string
		resume *ResumeState
		want   bool
	}{
		{"plain SEGMENTS fetches", &ResumeState{Stage: StageSegments}, false},
		{"PREPARE_INPUT skips", &ResumeState{Stage: StagePrepareInput}, true},
		{"pending discontinuity split in SEGMENTS skips", &ResumeState{Stage: StageSegments, PendingSplit: true}, true},
		{"pending threshold split in SEGMENTS skips", &ResumeState{Stage: StageSegments, PendingSplit: true, PendingThresholdSplit: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSkipSegmentFetch(tc.resume); got != tc.want {
				t.Errorf("shouldSkipSegmentFetch = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRecoverLegacySeqZeroPartStartedFromDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "0.ts"), []byte("media"), 0o644); err != nil {
		t.Fatalf("write 0.ts: %v", err)
	}
	resume := &ResumeState{
		Stage:                     StageSegments,
		CurrentPartIndex:          1,
		PartStartMediaSequence:    0,
		AccountedFrontierMediaSeq: 0,
	}

	if !recoverLegacySeqZeroPartStarted(dir, resume) {
		t.Fatal("recoverLegacySeqZeroPartStarted=false with durable 0.ts; want true")
	}
	if !resume.PartStarted {
		t.Fatal("PartStarted=false after legacy seq-0 disk recovery")
	}
	if resume.SegmentFormat != string(hls.SegmentKindTS) {
		t.Fatalf("SegmentFormat=%q, want ts", resume.SegmentFormat)
	}
	if !hasPartContent(nil, resume) {
		t.Fatal("hasPartContent=false after legacy seq-0 disk recovery")
	}
}

func TestRecoverLegacySeqZeroPartStartedRequiresSegmentFile(t *testing.T) {
	resume := &ResumeState{
		Stage:                     StageSegments,
		CurrentPartIndex:          1,
		PartStartMediaSequence:    0,
		AccountedFrontierMediaSeq: 0,
	}

	if recoverLegacySeqZeroPartStarted(t.TempDir(), resume) {
		t.Fatal("recoverLegacySeqZeroPartStarted=true without 0.ts/0.m4s; would skip media seq 0 after a pre-poll crash")
	}
	if resume.PartStarted {
		t.Fatal("PartStarted=true without durable seq-0 media file")
	}
}

func TestPendingSplitEndedAtBoundary(t *testing.T) {
	cases := []struct {
		name      string
		resume    *ResumeState
		hlsResult *hls.JobResult
		want      bool
	}{
		{
			name:   "no pending split",
			resume: &ResumeState{EndListSeen: true},
			want:   false,
		},
		{
			name:      "pending discontinuity split with ENDLIST",
			resume:    &ResumeState{PendingSplit: true, EndListSeen: true},
			hlsResult: &hls.JobResult{EndList: true},
			want:      true,
		},
		{
			name: "threshold boundary is observed tail",
			resume: &ResumeState{
				PendingSplit:                  true,
				PendingThresholdSplit:         true,
				EndListSeen:                   true,
				PendingSplitBoundaryMediaSeq:  10,
				PendingSplitBoundarySet:       true,
				PendingSplitEndListAtBoundary: true,
			},
			hlsResult: &hls.JobResult{EndList: true, LastMediaSeq: 10},
			want:      true,
		},
		{
			name: "threshold boundary has queued tail",
			resume: &ResumeState{
				PendingSplit:                 true,
				PendingThresholdSplit:        true,
				EndListSeen:                  true,
				PendingSplitBoundaryMediaSeq: 10,
				PendingSplitBoundarySet:      true,
			},
			hlsResult: &hls.JobResult{EndList: true, LastMediaSeq: 12},
			want:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pendingSplitEndedAtBoundary(tc.resume, tc.hlsResult); got != tc.want {
				t.Errorf("pendingSplitEndedAtBoundary = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSynthesizeHLSResultFromResume_UsesThresholdBoundary(t *testing.T) {
	resume := &ResumeState{
		PartStarted:                  true,
		PartStartMediaSequence:       100,
		AccountedFrontierMediaSeq:    100,
		PendingThresholdSplit:        true,
		PendingSplitBoundaryMediaSeq: 100,
		PendingSplitBoundarySet:      true,
		CompletedAboveFrontier:       []int64{103},
	}
	got := synthesizeHLSResultFromResume(resume, hls.SegmentKindTS)
	if got.LastMediaSeq != 100 {
		t.Fatalf("LastMediaSeq=%d, want threshold boundary 100", got.LastMediaSeq)
	}
	if got.SegmentsDone != 1 {
		t.Fatalf("SegmentsDone=%d, want only boundary-inclusive current part", got.SegmentsDone)
	}
}

func TestThresholdSplitEndListPersistenceRequiresBoundaryProof(t *testing.T) {
	resume := &ResumeState{
		PendingSplit:                 true,
		PendingThresholdSplit:        true,
		PendingSplitBoundaryMediaSeq: 10,
		PendingSplitBoundarySet:      true,
	}

	atBoundary := &hls.JobResult{EndList: true, LastMediaSeq: 10}
	if !thresholdSplitEndListAtBoundary(resume, atBoundary) {
		t.Fatal("thresholdSplitEndListAtBoundary=false for ENDLIST exactly at boundary")
	}
	if !shouldPersistEndListSeen(resume, atBoundary) {
		t.Fatal("shouldPersistEndListSeen=false for ENDLIST exactly at threshold boundary")
	}

	withTail := &hls.JobResult{EndList: true, LastMediaSeq: 12}
	if thresholdSplitEndListAtBoundary(resume, withTail) {
		t.Fatal("thresholdSplitEndListAtBoundary=true with post-boundary tail")
	}
	if shouldPersistEndListSeen(resume, withTail) {
		t.Fatal("shouldPersistEndListSeen=true with post-boundary tail; want deferred until continuation captures it")
	}

	resume.EndListSeen = true
	resume.PendingSplitEndListAtBoundary = true
	synth := synthesizeHLSResultFromResume(resume, hls.SegmentKindTS)
	if !synth.EndList {
		t.Fatal("synthesized resume result lost durable ENDLIST-at-boundary proof")
	}
	if !pendingSplitEndedAtBoundary(resume, synth) {
		t.Fatal("pendingSplitEndedAtBoundary=false despite durable ENDLIST-at-boundary proof")
	}
}

func TestPendingThresholdSplitResumeWithEndListNeedsDurableTailProof(t *testing.T) {
	resume := &ResumeState{
		Stage:                        StageSegments,
		PartStarted:                  true,
		PartStartMediaSequence:       8,
		AccountedFrontierMediaSeq:    10,
		PendingSplit:                 true,
		PendingThresholdSplit:        true,
		PendingSplitBoundaryMediaSeq: 10,
		PendingSplitBoundarySet:      true,
		EndListSeen:                  true,
	}

	// This is the crash shape from the review finding: ENDLIST was
	// persisted while the part was still in SEGMENTS, but the durable
	// resume state does not record the HLS run's real final media
	// sequence. Synthesizing LastMediaSeq as the split boundary must not
	// be treated as proof that the broadcast ended exactly at the
	// boundary; otherwise resume can clear PendingSplit and drop the
	// continuation tail.
	synth := synthesizeHLSResultFromResume(resume, hls.SegmentKindTS)
	if pendingSplitEndedAtBoundary(resume, synth) {
		t.Fatalf("pendingSplitEndedAtBoundary=true with only synthesized boundary proof; want false so resume opens/refetches the continuation")
	}
}

func TestPruneSegmentsAfterBoundary(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"100.ts", "101.ts", "102.ts", "103.m4s", "init.mp4", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := pruneSegmentsAfterBoundary(dir, hls.SegmentKindTS, 101); err != nil {
		t.Fatalf("prune ts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "100.ts")); err != nil {
		t.Error("100.ts pruned, want kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "101.ts")); err != nil {
		t.Error("101.ts pruned, want kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "102.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("102.ts exists after prune, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "103.m4s")); err != nil {
		t.Error("m4s file pruned during TS prune, want untouched")
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
			name: "part threshold exceeded (bare sentinel)",
			err:  ErrPartThresholdExceeded,
			want: true,
		},
		{
			name: "part threshold exceeded (wrapped)",
			err:  fmt.Errorf("hls run: %w", fmt.Errorf("%w: part 2 reached size/duration ceiling", ErrPartThresholdExceeded)),
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

// TestMapForcedSplitErr pins the scoped-cancel → split-sentinel
// translation shared by the restart-gap and size/duration paths. The
// load-bearing case is "fired but parent ctx cancelled" (a shutdown
// racing the split): the sentinel must NOT be synthesized then, or it
// would mask the teardown — the split intent is already checkpointed
// for resume.
func TestMapForcedSplitErr(t *testing.T) {
	injected := errors.New("injected failure")
	live := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name     string
		ctx      context.Context
		err      error
		fired    bool
		sealed   bool
		sentinel error
		wantSent bool  // result must wrap ErrPartThresholdExceeded
		wantErr  error // expected passthrough err when !wantSent
	}{
		{"not fired — passthrough nil", live, nil, false, true, ErrPartThresholdExceeded, false, nil},
		{"not fired — passthrough err", live, injected, false, true, ErrPartThresholdExceeded, false, injected},
		{"fired but parent cancelled — passthrough (shutdown won)", dead, nil, true, true, ErrPartThresholdExceeded, false, nil},
		{"fired, sealed, live, nil err — synthesize threshold sentinel", live, nil, true, true, ErrPartThresholdExceeded, true, nil},
		{"fired, sealed, live, context.Canceled — synthesize threshold sentinel", live, context.Canceled, true, true, ErrPartThresholdExceeded, true, nil},
		{"fired, sealed, live, real err — sentinel wins (above-boundary, refetched)", live, injected, true, true, ErrPartThresholdExceeded, true, nil},
		// Hardening: a fired split WITHOUT a sealed boundary (restart-gap,
		// or a hypothetical threshold path that forgot to seal) must NOT
		// mask a genuine non-cancel failure.
		{"restart-gap fired (unsealed), live, real err — passthrough genuine failure", live, injected, true, false, ErrRestartGapExceeded, false, injected},
		{"restart-gap fired (unsealed), live, nil — synthesize", live, nil, true, false, ErrRestartGapExceeded, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapForcedSplitErr(tc.ctx, tc.err, tc.fired, tc.sealed, tc.sentinel, "ceiling")
			if tc.wantSent {
				if !errors.Is(got, tc.sentinel) {
					t.Errorf("got %v, want wrapped %v", got, tc.sentinel)
				}
				return
			}
			if got != tc.wantErr {
				t.Errorf("got %v, want passthrough %v", got, tc.wantErr)
			}
		})
	}
}

func TestMapForcedSplitErr_ThresholdSplitWinsPostBoundaryWorkerErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "gap abort from canceled above-boundary fetch",
			err: &hls.GapAbortError{
				Reason:  "no content segment committed yet",
				LastSeq: 101,
				LastErr: context.Canceled,
			},
		},
		{
			name: "gap abort from failed above-boundary fetch",
			err: &hls.GapAbortError{
				Reason:  "no content segment committed yet",
				LastSeq: 101,
				LastErr: errors.New("post-boundary fetch failed"),
			},
		},
		{
			name: "auth from above-boundary fetch",
			err:  hls.ErrPlaylistAuth,
		},
		{
			name: "variant change observed after threshold boundary",
			err:  fmt.Errorf("%w: quality %q -> %q", ErrVariantChanged, "720", "480"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapForcedSplitErr(
				context.Background(),
				tc.err,
				true,
				true, // boundary sealed — a real threshold split
				ErrPartThresholdExceeded,
				"part reached size/duration ceiling",
			)
			if !errors.Is(got, ErrPartThresholdExceeded) {
				t.Fatalf("got %v, want threshold split sentinel to win over post-boundary worker error %v", got, tc.err)
			}
		})
	}
}

// TestRefetchSeqsForNextAttempt covers P1 #2: a canceled in-flight fetch
// left unresolved must be carried into the next attempt's refetch set,
// not just the auth-errored seqs. Without it the canceled seq sits below
// the advanced startSeq forever — a permanent hole.
func TestRefetchSeqsForNextAttempt(t *testing.T) {
	cases := []struct {
		name     string
		authSeqs []int64
		canceled map[int64]bool
		want     []int64
	}{
		{"auth only", []int64{102}, map[int64]bool{}, []int64{102}},
		{"canceled below auth must be retried", []int64{102}, map[int64]bool{101: true}, []int64{101, 102}},
		{"both dimensions, deduped", []int64{102}, map[int64]bool{102: true, 100: true}, []int64{100, 102}},
		{"canceled only", nil, map[int64]bool{99: true, 101: true}, []int64{99, 101}},
		{"neither", nil, map[int64]bool{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := refetchSeqsForNextAttempt(tc.authSeqs, tc.canceled)
			if !slices.Equal(got, tc.want) {
				t.Errorf("refetchSeqsForNextAttempt(%v, %v) = %v, want %v", tc.authSeqs, tc.canceled, got, tc.want)
			}
		})
	}
}

// TestRefetchSeqsForNextAttempt_AfterFoldRetriesCanceledSeq mirrors the
// fetchWithAuthRefresh wiring end to end at the unit level: fold an
// attempt that canceled seq 101 while seq 102 auth-errored, then build the
// next attempt's refetch set. 101 must be included so the next hls.Run
// re-emits it; before the fix only [102] was carried and 101 (below the
// advanced startSeq 103) was lost.
func TestRefetchSeqsForNextAttempt_AfterFoldRetriesCanceledSeq(t *testing.T) {
	resume := NewResumeState()
	resume.StartPart(100)
	agg := &hls.JobResult{}
	unresolved := map[int64]bool{}

	result := &hls.JobResult{
		LastMediaSeq:     102,
		SegmentsCanceled: 1,
		CanceledSeqs:     []int64{101},
		AuthErrorSeqs:    []int64{102},
		SegmentsDone:     2,
	}
	foldHLSAttemptResult(agg, result, resume, unresolved)

	got := refetchSeqsForNextAttempt(result.AuthErrorSeqs, unresolved)
	if !slices.Contains(got, int64(101)) {
		t.Fatalf("next-attempt refetch set = %v, must include canceled seq 101 (else startSeq=%d skips it forever)", got, agg.LastMediaSeq+1)
	}
	if !slices.Contains(got, int64(102)) {
		t.Fatalf("next-attempt refetch set = %v, must include auth seq 102", got)
	}
}

func TestFoldHLSAttemptResult_EndListWaitsForCanceledSeqAcrossAuthRefresh(t *testing.T) {
	resume := NewResumeState()
	resume.StartPart(101)
	agg := &hls.JobResult{}
	unresolved := map[int64]bool{}

	foldHLSAttemptResult(agg, &hls.JobResult{
		LastMediaSeq:     102,
		SegmentsCanceled: 1,
		CanceledSeqs:     []int64{101},
		AuthErrorSeqs:    []int64{102},
		SegmentsDone:     1,
		SegmentsGaps:     1,
		BytesWritten:     100,
		Kind:             hls.SegmentKindTS,
	}, resume, unresolved)
	if len(unresolved) != 1 || !unresolved[101] {
		t.Fatalf("unresolved canceled set=%v, want seq 101 retained for auth-refresh aggregate", unresolved)
	}

	foldHLSAttemptResult(agg, &hls.JobResult{
		EndList:      true,
		LastMediaSeq: 103,
		SegmentsDone: 2,
	}, resume, unresolved)
	if agg.EndList {
		t.Fatal("aggregate EndList=true while canceled seq 101 is still unresolved")
	}

	resume.NoteCommitted(101)
	foldHLSAttemptResult(agg, &hls.JobResult{
		EndList:      true,
		LastMediaSeq: 103,
		SegmentsDone: 3,
	}, resume, unresolved)
	if !agg.EndList {
		t.Fatal("aggregate EndList=false after the canceled seq was durably resolved by a later attempt")
	}
	if len(unresolved) != 0 {
		t.Fatalf("unresolved canceled set=%v, want empty after seq 101 resolved", unresolved)
	}
}
