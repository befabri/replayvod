package downloader

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestContinuePart pins the size/duration-split continuation: unlike
// BeginNewPart (which zeroes the anchor for a new variant's
// independent counter), ContinuePart carries the frontier forward so
// part N+1 picks up at endSeq+1 in the SAME media-sequence space —
// no gap, no re-fetch — while keeping the variant lock and resetting
// per-part accounting. The marshal round-trip half is the crash-mid-
// split contract: a resumed process must skip the already-committed
// seqs rather than re-download them.
func TestContinuePart(t *testing.T) {
	fps := 30.0
	r := &ResumeState{
		CurrentPartIndex:             1,
		PartStarted:                  true,
		PartStartMediaSequence:       100,
		AccountedFrontierMediaSeq:    142,
		CompletedAboveFrontier:       []int64{145},
		Gaps:                         []Gap{{MediaSeq: 130, EndMediaSeq: 130, Reason: GapReasonFetchFailure}},
		SelectedQuality:              "720",
		SelectedFPS:                  &fps,
		SelectedCodec:                "avc1.4d401f",
		SegmentFormat:                "ts",
		PartBytes:                    5_000_000,
		PartDurationSeconds:          42.5,
		PendingSplit:                 true,
		PendingThresholdSplit:        true,
		PendingSplitBoundaryMediaSeq: 142,
		PendingSplitBoundarySet:      true,
		HadWindowRoll:                true,
		EndListSeen:                  true,
	}
	r.Init()

	r.ContinuePart()

	if r.CurrentPartIndex != 2 {
		t.Errorf("CurrentPartIndex = %d, want 2", r.CurrentPartIndex)
	}
	// Seq-continuous: part N+1 anchors exactly one past part N's end,
	// frontier sits at the boundary so the first NoteCommitted of
	// PartStart advances cleanly.
	if r.PartStartMediaSequence != 143 {
		t.Errorf("PartStartMediaSequence = %d, want 143 (endSeq+1)", r.PartStartMediaSequence)
	}
	if !r.PartStarted {
		t.Error("PartStarted=false after ContinuePart, want true")
	}
	if r.AccountedFrontierMediaSeq != 142 {
		t.Errorf("AccountedFrontierMediaSeq = %d, want 142 (carried, == endSeq)", r.AccountedFrontierMediaSeq)
	}
	// Per-part accounting resets.
	if r.CompletedAboveFrontier != nil {
		t.Errorf("CompletedAboveFrontier = %v, want nil", r.CompletedAboveFrontier)
	}
	if len(r.Gaps) != 0 {
		t.Errorf("Gaps = %v, want empty", r.Gaps)
	}
	if r.PartBytes != 0 || r.PartDurationSeconds != 0 {
		t.Errorf("accumulators not reset: bytes=%d seconds=%v", r.PartBytes, r.PartDurationSeconds)
	}
	// Split flags consumed.
	if r.PendingSplit || r.PendingThresholdSplit {
		t.Errorf("split flags not consumed: PendingSplit=%v PendingThresholdSplit=%v", r.PendingSplit, r.PendingThresholdSplit)
	}
	if r.PendingSplitBoundarySet || r.PendingSplitBoundaryMediaSeq != 0 {
		t.Errorf("split boundary not cleared: set=%v boundary=%d", r.PendingSplitBoundarySet, r.PendingSplitBoundaryMediaSeq)
	}
	// Variant lock RETAINED — same stream continues into the next part.
	if r.SelectedQuality != "720" || r.SelectedCodec != "avc1.4d401f" || r.SegmentFormat != "ts" ||
		r.SelectedFPS == nil || *r.SelectedFPS != 30.0 {
		t.Errorf("variant lock not retained: q=%q codec=%q fmt=%q fps=%v",
			r.SelectedQuality, r.SelectedCodec, r.SegmentFormat, r.SelectedFPS)
	}
	// Cross-part loss signal preserved, but a threshold continuation
	// means the full broadcast is not yet durably owned by finalized
	// parts. A stale EndListSeen from an older checkpoint must be
	// cleared so failures in the continuation still report truncated.
	if !r.HadWindowRoll {
		t.Error("HadWindowRoll cleared; must persist across parts")
	}
	if r.EndListSeen {
		t.Error("EndListSeen=true after ContinuePart without boundary ENDLIST proof; want false")
	}
	if r.Stage != StageAuth {
		t.Errorf("Stage = %q, want %q", r.Stage, StageAuth)
	}

	// Crash-mid-split contract: after a checkpoint round-trip the
	// resumed state still anchors at 143/142, so the resume path skips
	// the committed [100..142] and resumes at 143 — committed segments
	// are never re-fetched.
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalResumeState(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.PartStartMediaSequence != 143 || got.AccountedFrontierMediaSeq != 142 {
		t.Errorf("post-roundtrip anchor: partStart=%d frontier=%d, want 143/142",
			got.PartStartMediaSequence, got.AccountedFrontierMediaSeq)
	}
	if !got.ShouldSkip(120) || !got.ShouldSkip(142) {
		t.Error("resumed state must skip already-accounted seqs <= 142 (no re-fetch)")
	}
	if got.ShouldSkip(143) {
		t.Error("resumed state must NOT skip the continuation seq 143")
	}
}

func TestResumeState_NoteCommitted_InOrder(t *testing.T) {
	r := NewResumeState()
	for _, seq := range []int64{1, 2, 3, 4, 5} {
		r.NoteCommitted(seq)
	}
	if r.AccountedFrontierMediaSeq != 5 {
		t.Errorf("frontier=%d, want 5", r.AccountedFrontierMediaSeq)
	}
	if len(r.CompletedAboveFrontier) != 0 {
		t.Errorf("CompletedAboveFrontier=%v, want empty (all consumed)", r.CompletedAboveFrontier)
	}
}

func TestResumeState_NoteCommitted_OutOfOrder(t *testing.T) {
	r := NewResumeState()
	// Workers complete out of order: 3 arrives before 1 and 2.
	r.NoteCommitted(3)
	if r.AccountedFrontierMediaSeq != 0 {
		t.Errorf("frontier=%d after seq=3, want 0 (1 and 2 missing)", r.AccountedFrontierMediaSeq)
	}
	if !slices.Equal(r.CompletedAboveFrontier, []int64{3}) {
		t.Errorf("CompletedAboveFrontier=%v, want [3]", r.CompletedAboveFrontier)
	}
	// 1 arrives — frontier advances to 1 only.
	r.NoteCommitted(1)
	if r.AccountedFrontierMediaSeq != 1 {
		t.Errorf("frontier=%d after seq=1, want 1", r.AccountedFrontierMediaSeq)
	}
	// 2 arrives — frontier skips to 3 as the gap fills.
	r.NoteCommitted(2)
	if r.AccountedFrontierMediaSeq != 3 {
		t.Errorf("frontier=%d after seq=2, want 3 (3 was already resolved)", r.AccountedFrontierMediaSeq)
	}
	if len(r.CompletedAboveFrontier) != 0 {
		t.Errorf("CompletedAboveFrontier=%v, want empty", r.CompletedAboveFrontier)
	}
}

func TestResumeState_NoteCommittedSegment_AccountsOnlyContiguousFrontier(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)

	r.NoteCommittedSegment(103, 30, 1)
	if r.PartBytes != 0 || r.PartDurationSeconds != 0 {
		t.Fatalf("out-of-order commit was counted early: bytes=%d seconds=%v", r.PartBytes, r.PartDurationSeconds)
	}
	if !slices.Equal(r.CompletedAboveFrontier, []int64{103}) {
		t.Fatalf("CompletedAboveFrontier=%v, want [103]", r.CompletedAboveFrontier)
	}

	r.NoteCommittedSegment(100, 10, 1)
	if r.AccountedFrontierMediaSeq != 100 {
		t.Fatalf("frontier=%d, want 100", r.AccountedFrontierMediaSeq)
	}
	if r.PartBytes != 10 || r.PartDurationSeconds != 1 {
		t.Fatalf("contiguous seq 100 not counted: bytes=%d seconds=%v", r.PartBytes, r.PartDurationSeconds)
	}

	r.NoteCommittedSegment(101, 11, 1)
	r.NoteCommittedSegment(102, 12, 1)
	if r.AccountedFrontierMediaSeq != 103 {
		t.Fatalf("frontier=%d, want 103 after consuming above-frontier seq", r.AccountedFrontierMediaSeq)
	}
	if r.PartBytes != 63 || r.PartDurationSeconds != 4 {
		t.Fatalf("contiguous accounting = %d/%v, want 63/4", r.PartBytes, r.PartDurationSeconds)
	}
	if len(r.CompletedAboveFrontier) != 0 || len(r.CompletedAboveFrontierAccounting) != 0 {
		t.Fatalf("above-frontier state not consumed: seqs=%v accounting=%v", r.CompletedAboveFrontier, r.CompletedAboveFrontierAccounting)
	}
}

func TestResumeState_NoteCommittedSegmentUntilThreshold_StopsAtFirstCrossing(t *testing.T) {
	r := NewResumeState()
	r.StartPart(0)

	for _, seq := range []int64{1, 2, 3} {
		boundary, crossed := r.NoteCommittedSegmentUntilThreshold(seq, 10, 1, 15, 0)
		if crossed {
			t.Fatalf("seq %d crossed early at boundary %d", seq, boundary)
		}
	}
	if r.AccountedFrontierMediaSeq != -1 {
		t.Fatalf("frontier=%d, want -1 while seq 0 is unresolved", r.AccountedFrontierMediaSeq)
	}

	boundary, crossed := r.NoteCommittedSegmentUntilThreshold(0, 10, 1, 15, 0)
	if !crossed || boundary != 1 {
		t.Fatalf("boundary=%d crossed=%v, want 1/true", boundary, crossed)
	}
	if r.AccountedFrontierMediaSeq != 1 {
		t.Fatalf("frontier=%d, want boundary 1", r.AccountedFrontierMediaSeq)
	}
	if r.PartBytes != 20 || r.PartDurationSeconds != 2 {
		t.Fatalf("part accounting=%d/%v, want 20/2 at boundary", r.PartBytes, r.PartDurationSeconds)
	}
	if !slices.Equal(r.CompletedAboveFrontier, []int64{2, 3}) {
		t.Fatalf("CompletedAboveFrontier=%v, want [2 3] left for boundary pruning", r.CompletedAboveFrontier)
	}

	r.SealThresholdSplitBoundary(boundary)
	if len(r.CompletedAboveFrontier) != 0 || len(r.CompletedAboveFrontierAccounting) != 0 {
		t.Fatalf("above-boundary state survived seal: seqs=%v accounting=%v", r.CompletedAboveFrontier, r.CompletedAboveFrontierAccounting)
	}
	if r.ShouldSkip(2) || r.ShouldSkip(3) {
		t.Fatalf("above-boundary seqs should be refetched by continuation")
	}
}

func TestResumeState_CompletedAccountingSerializesFromMap(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)

	r.NoteCommittedSegment(101, 11, 1)
	if len(r.CompletedAboveFrontierAccounting) != 0 {
		t.Fatalf("runtime CompletedAboveFrontierAccounting=%v, want empty; map is the hot-path source of truth", r.CompletedAboveFrontierAccounting)
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	restored, err := UnmarshalResumeState(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	restored.NoteCommittedSegment(100, 10, 1)
	if restored.AccountedFrontierMediaSeq != 101 {
		t.Fatalf("frontier=%d, want 101 after serialized above-frontier accounting is consumed", restored.AccountedFrontierMediaSeq)
	}
	if restored.PartBytes != 21 || restored.PartDurationSeconds != 2 {
		t.Fatalf("part accounting=%d/%v, want 21/2 from seq 100 plus serialized seq 101", restored.PartBytes, restored.PartDurationSeconds)
	}
}

func TestResumeState_NoteGapUntilThreshold_StopsAtFirstCrossingAfterLowerGap(t *testing.T) {
	r := NewResumeState()
	r.StartPart(0)

	r.NoteCommittedSegmentUntilThreshold(1, 10, 1, 10, 0)
	r.NoteCommittedSegmentUntilThreshold(2, 10, 1, 10, 0)

	boundary, crossed := r.NoteGapUntilThreshold(0, GapReasonStitchedAd, 10, 0)
	if !crossed || boundary != 1 {
		t.Fatalf("boundary=%d crossed=%v, want 1/true", boundary, crossed)
	}
	if r.AccountedFrontierMediaSeq != 1 {
		t.Fatalf("frontier=%d, want boundary 1", r.AccountedFrontierMediaSeq)
	}
	if r.PartBytes != 10 || r.PartDurationSeconds != 1 {
		t.Fatalf("part accounting=%d/%v, want 10/1 at boundary", r.PartBytes, r.PartDurationSeconds)
	}
	if !slices.Equal(r.CompletedAboveFrontier, []int64{2}) {
		t.Fatalf("CompletedAboveFrontier=%v, want [2] left for boundary pruning", r.CompletedAboveFrontier)
	}
}

// TestResumeState_NoteRangeGapUntilThreshold_SealsBoundaryOnWindowRollFold
// is the window-roll analogue of the single-gap threshold test. A crash
// can leave committed segments buffered above an unfilled hole; on resume
// the playlist has rolled past the missing lower seqs, so OnWindowRoll
// fills the whole range at once. That fold makes the buffered commits
// contiguous and can push the part over max_part_*. The range-gap path
// must seal the first crossing seq exactly like the single-gap path,
// rather than advancing the frontier with the ceiling disabled and
// silently overshooting.
func TestResumeState_NoteRangeGapUntilThreshold_SealsBoundaryOnWindowRollFold(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)

	// Concurrent workers committed 103..105 before the lower seqs, then
	// the process crashed. 100..102 are still missing, so the frontier
	// can't advance and nothing folds into the part totals yet.
	for _, seq := range []int64{103, 104, 105} {
		r.NoteCommittedSegment(seq, 10, 1)
	}
	if r.AccountedFrontierMediaSeq != 99 {
		t.Fatalf("frontier=%d, want 99 while 100..102 unresolved", r.AccountedFrontierMediaSeq)
	}
	if r.PartBytes != 0 || r.PartDurationSeconds != 0 {
		t.Fatalf("buffered above-frontier commits counted early: bytes=%d seconds=%v", r.PartBytes, r.PartDurationSeconds)
	}

	// On resume the window rolled past 100..102; the fill makes 103..105
	// contiguous and folds their bytes. A 15-byte ceiling is crossed at
	// 104 (10+10), so the cut seals there with 105 left above for refetch.
	boundary, crossed := r.NoteRangeGapUntilThreshold(100, 102, GapReasonRestartWindowRolled, 15, 0)
	if !crossed {
		t.Fatal("crossed=false; a window-roll fold over the ceiling must report a boundary")
	}
	if boundary != 104 {
		t.Fatalf("boundary=%d, want 104 (first seq crossing the 15-byte ceiling)", boundary)
	}
	if r.AccountedFrontierMediaSeq != 104 {
		t.Fatalf("frontier=%d, want 104 (stopped at the boundary, not the end of the buffered run)", r.AccountedFrontierMediaSeq)
	}
	if r.PartBytes != 20 || r.PartDurationSeconds != 2 {
		t.Fatalf("part totals at boundary = %d/%v, want 20/2", r.PartBytes, r.PartDurationSeconds)
	}
	if !slices.Equal(r.CompletedAboveFrontier, []int64{105}) {
		t.Fatalf("CompletedAboveFrontier=%v, want [105] left for the continuation", r.CompletedAboveFrontier)
	}

	// Sealing drops the above-boundary commit so the continuation
	// refetches 105 from boundary+1 instead of treating it as
	// already-owned current-part work.
	r.SealThresholdSplitBoundary(boundary)
	if len(r.CompletedAboveFrontier) != 0 || len(r.CompletedAboveFrontierAccounting) != 0 {
		t.Fatalf("above-boundary state survived seal: seqs=%v accounting=%v",
			r.CompletedAboveFrontier, r.CompletedAboveFrontierAccounting)
	}
	if r.ShouldSkip(105) {
		t.Fatal("seq 105 must be refetched by the continuation, not skipped")
	}
	if !r.ShouldSkip(104) {
		t.Fatal("seq 104 is at/below the boundary and already owned by this part")
	}
}

// TestResumeState_NoteRangeGap_DisabledThresholdUnchanged pins that the
// 0/0 wrapper still advances the whole contiguous run (no boundary) so
// the non-threshold window-roll path is byte-for-byte the prior behavior.
func TestResumeState_NoteRangeGap_DisabledThresholdUnchanged(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	for _, seq := range []int64{103, 104, 105} {
		r.NoteCommittedSegment(seq, 10, 1)
	}

	r.NoteRangeGap(100, 102, GapReasonRestartWindowRolled)

	if r.AccountedFrontierMediaSeq != 105 {
		t.Fatalf("frontier=%d, want 105 (disabled ceiling advances the whole run)", r.AccountedFrontierMediaSeq)
	}
	if r.PartBytes != 30 || r.PartDurationSeconds != 3 {
		t.Fatalf("part totals = %d/%v, want 30/3 (all buffered commits folded)", r.PartBytes, r.PartDurationSeconds)
	}
	if len(r.CompletedAboveFrontier) != 0 {
		t.Fatalf("CompletedAboveFrontier=%v, want empty after full advance", r.CompletedAboveFrontier)
	}
}

func TestResumeState_SealThresholdSplitBoundary_DropsAboveBoundaryAccounting(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.NoteCommittedSegment(103, 30, 1)
	r.NoteCommittedSegment(100, 10, 1)

	r.PendingSplit = true
	r.PendingThresholdSplit = true
	r.SealThresholdSplitBoundary(r.AccountedFrontierMediaSeq)

	if !r.PendingSplitBoundarySet || r.PendingSplitBoundaryMediaSeq != 100 {
		t.Fatalf("boundary = %d set=%v, want 100/true", r.PendingSplitBoundaryMediaSeq, r.PendingSplitBoundarySet)
	}
	if len(r.CompletedAboveFrontier) != 0 || len(r.CompletedAboveFrontierAccounting) != 0 {
		t.Fatalf("above-boundary accounting survived seal: seqs=%v accounting=%v", r.CompletedAboveFrontier, r.CompletedAboveFrontierAccounting)
	}
	if r.ShouldSkip(103) {
		t.Fatal("seq 103 should be refetched by the next part, not skipped as current-part work")
	}
}

func TestResumeState_NoteCommitted_Idempotent(t *testing.T) {
	r := NewResumeState()
	r.NoteCommitted(5)
	r.NoteCommitted(5)
	r.NoteCommitted(5)
	if !slices.Equal(r.CompletedAboveFrontier, []int64{5}) {
		t.Errorf("CompletedAboveFrontier=%v, want [5] (duplicates rejected)", r.CompletedAboveFrontier)
	}
}

func TestResumeState_NoteCommitted_BelowFrontierIgnored(t *testing.T) {
	r := NewResumeState()
	for _, seq := range []int64{1, 2, 3} {
		r.NoteCommitted(seq)
	}
	r.NoteCommitted(2) // already consumed
	if r.AccountedFrontierMediaSeq != 3 {
		t.Errorf("frontier=%d, want 3 (unchanged)", r.AccountedFrontierMediaSeq)
	}
	if len(r.Gaps) != 0 {
		t.Errorf("Gaps=%v, want none", r.Gaps)
	}
}

func TestResumeState_NoteGap_AdvancesFrontier(t *testing.T) {
	r := NewResumeState()
	r.NoteCommitted(1)
	r.NoteCommitted(2)
	r.NoteGap(3, GapReasonFetchFailure)
	r.NoteCommitted(4)
	if r.AccountedFrontierMediaSeq != 4 {
		t.Errorf("frontier=%d, want 4 (gap at 3 accounted)", r.AccountedFrontierMediaSeq)
	}
	if len(r.Gaps) != 1 {
		t.Fatalf("len(Gaps)=%d, want 1", len(r.Gaps))
	}
	if r.Gaps[0].MediaSeq != 3 || r.Gaps[0].EndMediaSeq != 3 {
		t.Errorf("Gaps[0]=%+v, want single-seq gap at 3", r.Gaps[0])
	}
	if r.Gaps[0].Reason != GapReasonFetchFailure {
		t.Errorf("Gaps[0].Reason=%q, want %q", r.Gaps[0].Reason, GapReasonFetchFailure)
	}
}

func TestResumeState_NoteGap_AheadOfFrontier(t *testing.T) {
	r := NewResumeState()
	// Ad skip at seq 5 arrives before seqs 1-4 complete.
	r.NoteGap(5, GapReasonStitchedAd)
	if r.AccountedFrontierMediaSeq != 0 {
		t.Errorf("frontier=%d, want 0 (1-4 still unresolved)", r.AccountedFrontierMediaSeq)
	}
	for _, seq := range []int64{1, 2, 3, 4} {
		r.NoteCommitted(seq)
	}
	if r.AccountedFrontierMediaSeq != 5 {
		t.Errorf("frontier=%d, want 5 (gap at 5 consumed after fill)", r.AccountedFrontierMediaSeq)
	}
}

func TestResumeState_NoteRangeGap_WindowRolled(t *testing.T) {
	r := NewResumeState()
	r.StartPart(10)
	r.NoteCommitted(10)
	r.NoteCommitted(11)
	// Restart: playlist head is now 50, lost [12, 49].
	r.NoteRangeGap(12, 49, GapReasonRestartWindowRolled)
	if r.AccountedFrontierMediaSeq != 49 {
		t.Errorf("frontier=%d, want 49 (range gap consumed contiguously)", r.AccountedFrontierMediaSeq)
	}
	r.NoteCommitted(50)
	if r.AccountedFrontierMediaSeq != 50 {
		t.Errorf("frontier=%d after resume at 50, want 50", r.AccountedFrontierMediaSeq)
	}
	if len(r.Gaps) != 1 {
		t.Fatalf("len(Gaps)=%d, want 1", len(r.Gaps))
	}
	if r.Gaps[0].MediaSeq != 12 || r.Gaps[0].EndMediaSeq != 49 {
		t.Errorf("Gaps[0]=%+v, want [12..49]", r.Gaps[0])
	}
}

func TestResumeState_NoteRangeGap_TrimsOverlap(t *testing.T) {
	r := NewResumeState()
	r.StartPart(10)
	r.NoteCommitted(10)
	// Range that overlaps already-committed 10 — trim start.
	r.NoteRangeGap(5, 15, GapReasonRestartWindowRolled)
	if r.AccountedFrontierMediaSeq != 15 {
		t.Errorf("frontier=%d, want 15", r.AccountedFrontierMediaSeq)
	}
	if r.Gaps[0].MediaSeq != 11 {
		t.Errorf("Gaps[0].MediaSeq=%d, want 11 (trimmed to frontier+1)", r.Gaps[0].MediaSeq)
	}
}

func TestResumeState_NoteRangeGap_EmptyRangeNoop(t *testing.T) {
	r := NewResumeState()
	r.NoteRangeGap(10, 5, GapReasonRestartWindowRolled) // inverted
	if r.AccountedFrontierMediaSeq != 0 {
		t.Errorf("frontier=%d, want 0", r.AccountedFrontierMediaSeq)
	}
	if len(r.Gaps) != 0 {
		t.Errorf("Gaps=%v, want none", r.Gaps)
	}
}

func TestResumeState_ShouldSkip(t *testing.T) {
	r := NewResumeState()
	r.NoteCommitted(1)
	r.NoteGap(2, GapReasonFetchFailure)
	r.NoteCommitted(3)
	r.NoteCommitted(5) // out of order; seq 4 missing
	r.NoteRangeGap(10, 12, GapReasonRestartWindowRolled)

	cases := []struct {
		seq  int64
		want bool
	}{
		{1, true},   // below frontier (now 3)
		{2, true},   // gap
		{3, true},   // below frontier
		{4, false},  // not resolved yet
		{5, true},   // in CompletedAboveFrontier
		{10, true},  // in range gap
		{11, true},  // in range gap
		{12, true},  // in range gap
		{13, false}, // outside range
	}
	for _, tc := range cases {
		if got := r.ShouldSkip(tc.seq); got != tc.want {
			t.Errorf("ShouldSkip(%d)=%v, want %v", tc.seq, got, tc.want)
		}
	}

	skip := r.SkipSet()
	for _, tc := range cases {
		if tc.seq > r.AccountedFrontierMediaSeq {
			if got := skip[tc.seq]; got != tc.want {
				t.Errorf("SkipSet[%d]=%v, want %v", tc.seq, got, tc.want)
			}
		}
	}
}

func TestResumeState_JSONRoundtrip(t *testing.T) {
	r := NewResumeState()
	r.SetStage(StageSegments)
	r.SelectedQuality = "720"
	r.SelectedCodec = "h264"
	r.SegmentFormat = "ts"
	r.StartPart(42)
	r.NoteCommitted(42)
	r.NoteCommitted(43)
	r.NoteGap(44, GapReasonStitchedAd)
	r.NoteCommitted(45)
	r.NoteCommitted(47) // out-of-order; 46 not yet in
	r.InitSegmentPath = "/tmp/j/segments/init.mp4"

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	restored, err := UnmarshalResumeState(data)
	if err != nil {
		t.Fatalf("UnmarshalResumeState: %v", err)
	}

	if restored.Stage != StageSegments {
		t.Errorf("Stage=%q, want %q", restored.Stage, StageSegments)
	}
	if restored.AccountedFrontierMediaSeq != r.AccountedFrontierMediaSeq {
		t.Errorf("frontier=%d, want %d", restored.AccountedFrontierMediaSeq, r.AccountedFrontierMediaSeq)
	}
	if !slices.Equal(restored.CompletedAboveFrontier, r.CompletedAboveFrontier) {
		t.Errorf("CompletedAboveFrontier=%v, want %v", restored.CompletedAboveFrontier, r.CompletedAboveFrontier)
	}
	if len(restored.Gaps) != len(r.Gaps) {
		t.Errorf("len(Gaps)=%d, want %d", len(restored.Gaps), len(r.Gaps))
	}
	if restored.InitSegmentPath != r.InitSegmentPath {
		t.Errorf("InitSegmentPath=%q, want %q", restored.InitSegmentPath, r.InitSegmentPath)
	}

	// Init must have rebuilt resolvedAbove from the on-wire data:
	// seq 47 should still be in the skip set, seq 46 should not.
	if !restored.ShouldSkip(47) {
		t.Error("restored state should skip seq 47 (in CompletedAboveFrontier)")
	}
	if restored.ShouldSkip(46) {
		t.Error("restored state should not skip seq 46 (not resolved)")
	}

	// Advance must still work post-restore: writing seq 46
	// should advance the frontier from 45 to 47.
	restored.NoteCommitted(46)
	if restored.AccountedFrontierMediaSeq != 47 {
		t.Errorf("post-restore frontier=%d, want 47", restored.AccountedFrontierMediaSeq)
	}
}

func TestUnmarshalResumeState_PrepareInputSeqZeroInfersPartStarted(t *testing.T) {
	raw := []byte(`{
		"stage": "PREPARE_INPUT",
		"current_part_index": 1,
		"part_start_media_sequence": 0,
		"accounted_frontier_media_seq": 0
	}`)

	r, err := UnmarshalResumeState(raw)
	if err != nil {
		t.Fatalf("UnmarshalResumeState: %v", err)
	}

	// PREPARE_INPUT and later are unambiguously post-fetch, so legacy
	// seq-0 checkpoints at these stages must infer PartStarted even
	// without the modern explicit field.
	if !r.PartStarted {
		t.Fatal("PartStarted=false for legacy PREPARE_INPUT checkpoint at media seq 0; want inferred true")
	}
	if !hasPartContent(nil, r) {
		t.Fatal("hasPartContent=false for legacy seq-0 checkpoint; want true so resume does not re-bootstrap")
	}
}

func TestUnmarshalResumeState_EmptyInput(t *testing.T) {
	for _, in := range []string{"", "{}"} {
		r, err := UnmarshalResumeState([]byte(in))
		if err != nil {
			t.Errorf("UnmarshalResumeState(%q): %v", in, err)
			continue
		}
		if r == nil {
			t.Errorf("UnmarshalResumeState(%q) returned nil", in)
			continue
		}
		// Should behave like a freshly-constructed state.
		r.NoteCommitted(1)
		if r.AccountedFrontierMediaSeq != 1 {
			t.Errorf("NoteCommitted after empty-input unmarshal: frontier=%d, want 1", r.AccountedFrontierMediaSeq)
		}
	}
}

func TestUnmarshalResumeState_InvalidJSON(t *testing.T) {
	_, err := UnmarshalResumeState([]byte("{not json"))
	if err == nil {
		t.Error("want error on invalid JSON")
	}
}

// TestResumeState_NoteCommittedClearsMatchingGap covers the
// auth-refetch path: the orchestrator first records a seq as a
// single-seq gap (GapReasonAuth) when the 401 fires, then later
// re-commits it once the fresh URL lands. The final state must
// show it as committed (no stale gap entry) so the resume record
// matches what's actually on disk.
func TestResumeState_NoteCommittedClearsMatchingGap(t *testing.T) {
	r := NewResumeState()
	r.NoteCommitted(1)
	r.NoteGap(2, GapReasonAuth) // auth error — seq 2 marked as gap, frontier advances to 2
	r.NoteCommitted(3)
	if r.AccountedFrontierMediaSeq != 3 {
		t.Fatalf("frontier=%d, want 3", r.AccountedFrontierMediaSeq)
	}
	if len(r.Gaps) != 1 {
		t.Fatalf("Gaps=%v, want one gap at 2", r.Gaps)
	}

	// Refetch succeeds for seq 2 — NoteCommitted must clear the
	// prior gap entry.
	r.NoteCommitted(2)
	if len(r.Gaps) != 0 {
		t.Errorf("Gaps=%v, want empty after refetch commit", r.Gaps)
	}
	// Frontier unchanged — seq 2 was already consumed by the
	// gap-accepted advance.
	if r.AccountedFrontierMediaSeq != 3 {
		t.Errorf("frontier=%d, want 3 (unchanged)", r.AccountedFrontierMediaSeq)
	}
}

// TestResumeState_NoteCommittedLeavesRangeGapAlone guards the
// boundary between single-seq gap removal (refetch) and range
// gaps (window-roll). A range gap covers many seqs recorded as a
// single entry; partial refetches of individual seqs inside that
// range shouldn't strip the whole record.
func TestResumeState_NoteCommittedLeavesRangeGapAlone(t *testing.T) {
	r := NewResumeState()
	r.StartPart(10)
	r.NoteCommitted(10)
	r.NoteRangeGap(11, 15, GapReasonRestartWindowRolled)
	if len(r.Gaps) != 1 || r.Gaps[0].EndMediaSeq != 15 {
		t.Fatalf("setup wrong: gaps=%v", r.Gaps)
	}
	// Pretend we somehow re-committed seq 13 (not a supported
	// path today, but the resume-state API shouldn't silently
	// eat the whole range if called).
	r.NoteCommitted(13)
	if len(r.Gaps) != 1 {
		t.Errorf("Gaps=%v, want range gap preserved", r.Gaps)
	}
	if r.Gaps[0].MediaSeq != 11 || r.Gaps[0].EndMediaSeq != 15 {
		t.Errorf("range gap altered: %+v", r.Gaps[0])
	}
}

// TestResumeState_AuthGapSeqs validates the restart-seed path:
// a resumed job with GapReasonAuth entries in resume_state must
// expose those mediaSeqs so fetchWithAuthRefresh's first attempt
// can refetch them. Other gap reasons (stitched-ad, window-roll,
// fetch-failure) must NOT appear — those aren't auth-retryable.
func TestResumeState_AuthGapSeqs(t *testing.T) {
	r := NewResumeState()
	r.NoteCommitted(1)
	r.NoteGap(2, GapReasonAuth)
	r.NoteCommitted(3)
	r.NoteGap(4, GapReasonStitchedAd)
	r.NoteGap(5, GapReasonAuth)
	r.NoteGap(6, GapReasonFetchFailure)
	r.NoteRangeGap(100, 110, GapReasonRestartWindowRolled)

	got := r.AuthGapSeqs()
	if len(got) != 2 {
		t.Fatalf("len(AuthGapSeqs)=%d, want 2", len(got))
	}
	// Order comes from Gaps[] insertion order; don't depend on it.
	if !slices.Contains(got, int64(2)) || !slices.Contains(got, int64(5)) {
		t.Errorf("AuthGapSeqs=%v, want {2, 5}", got)
	}

	// Round-trip through JSON: AuthGapSeqs must still work after
	// Unmarshal + Init rebuild.
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	restored, err := UnmarshalResumeState(data)
	if err != nil {
		t.Fatalf("UnmarshalResumeState: %v", err)
	}
	got2 := restored.AuthGapSeqs()
	if len(got2) != 2 {
		t.Errorf("post-restore len(AuthGapSeqs)=%d, want 2", len(got2))
	}
}

func TestResumeState_AuthGapSeqs_EmptyWhenNoGaps(t *testing.T) {
	r := NewResumeState()
	if got := r.AuthGapSeqs(); len(got) != 0 {
		t.Errorf("AuthGapSeqs=%v, want empty for fresh state", got)
	}
	r.NoteCommitted(1)
	if got := r.AuthGapSeqs(); len(got) != 0 {
		t.Errorf("AuthGapSeqs=%v, want empty when no gaps recorded", got)
	}
}

// TestResumeState_AuthGapSeqs_AfterRefetchSuccessIsEmpty pins the
// integration: a Gap{reason:auth_error} followed by a successful
// NoteCommitted (refetch) must leave AuthGapSeqs empty. Without
// this, a seq would loop across restarts even after it landed on
// disk.
func TestResumeState_AuthGapSeqs_AfterRefetchSuccessIsEmpty(t *testing.T) {
	r := NewResumeState()
	r.NoteCommitted(1)
	r.NoteGap(2, GapReasonAuth)
	if got := r.AuthGapSeqs(); !slices.Equal(got, []int64{2}) {
		t.Fatalf("pre-refetch AuthGapSeqs=%v, want [2]", got)
	}
	// Refetch succeeds.
	r.NoteCommitted(2)
	if got := r.AuthGapSeqs(); len(got) != 0 {
		t.Errorf("post-refetch AuthGapSeqs=%v, want empty", got)
	}
}

func TestStage_AtOrAfter(t *testing.T) {
	cases := []struct {
		stage  Stage
		target Stage
		want   bool
	}{
		// Fresh job: AtOrAfter(PREPARE_INPUT) is false — don't
		// skip the fetch.
		{StageAuth, StagePrepareInput, false},
		{StagePlaylist, StagePrepareInput, false},
		{StageSegments, StagePrepareInput, false},
		// Resumed jobs past SEGMENTS: skip the fetch.
		{StagePrepareInput, StagePrepareInput, true},
		{StageRemux, StagePrepareInput, true},
		{StageProbe, StagePrepareInput, true},
		{StageCorruptionCheck, StagePrepareInput, true},
		{StageThumbnail, StagePrepareInput, true},
		{StageStore, StagePrepareInput, true},
		// Self-equality.
		{StageAuth, StageAuth, true},
		{StageSegments, StageSegments, true},
		// Unknown stages (future or corrupted) fall back to
		// order=0, which re-runs from the start.
		{Stage("BOGUS"), StagePrepareInput, false},
	}
	for _, tc := range cases {
		if got := tc.stage.AtOrAfter(tc.target); got != tc.want {
			t.Errorf("Stage(%q).AtOrAfter(%q)=%v, want %v", tc.stage, tc.target, got, tc.want)
		}
	}
}

// TestResumeState_BeginNewPart: anchor zeroed (so OnFirstPoll
// re-anchors), variant lock cleared (so Stage 3 picks freely),
// PendingSplit cleared (so the outer loop terminates), Gaps and
// HadWindowRoll preserved (cross-part completion_kind signal).
func TestResumeState_BeginNewPart(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.NoteCommitted(100)
	r.NoteCommitted(101)
	r.NoteCommitted(105)
	r.NoteRangeGap(110, 112, GapReasonRestartWindowRolled)
	r.SelectedQuality = "1080"
	r.SelectedCodec = "h264"
	r.SegmentFormat = "ts"
	r.SetStage(StageSegments)
	r.PendingSplit = true
	r.HadWindowRoll = true
	r.EndListSeen = true // stale broadcast-ended marker from the sealed part

	priorPart := r.CurrentPartIndex
	priorGapsLen := len(r.Gaps)

	r.BeginNewPart()

	// A discontinuity part means the broadcast continues, so the prior
	// part's ENDLIST observation must not leak forward (else the new
	// part's failure would mis-report the recording as complete).
	if r.EndListSeen {
		t.Error("EndListSeen=true after BeginNewPart; a new discontinuity part must clear the stale broadcast-ended marker")
	}

	if r.CurrentPartIndex != priorPart+1 {
		t.Errorf("CurrentPartIndex=%d, want %d", r.CurrentPartIndex, priorPart+1)
	}
	if r.PartStartMediaSequence != 0 {
		t.Errorf("PartStartMediaSequence=%d, want 0 (next OnFirstPoll re-anchors)", r.PartStartMediaSequence)
	}
	if r.PartStarted {
		t.Error("PartStarted=true after BeginNewPart, want false until OnFirstPoll re-anchors")
	}
	if r.AccountedFrontierMediaSeq != 0 {
		t.Errorf("AccountedFrontierMediaSeq=%d, want 0 (next OnFirstPoll re-anchors)", r.AccountedFrontierMediaSeq)
	}
	if len(r.CompletedAboveFrontier) != 0 {
		t.Errorf("CompletedAboveFrontier=%v, want empty (per-part state, scoped to prior part)", r.CompletedAboveFrontier)
	}
	if r.SelectedQuality != "" || r.SelectedCodec != "" || r.SegmentFormat != "" {
		t.Errorf("variant lock not cleared: quality=%q codec=%q format=%q",
			r.SelectedQuality, r.SelectedCodec, r.SegmentFormat)
	}
	if r.Stage != StageAuth {
		t.Errorf("Stage=%q, want %q (next part re-runs Stages 1-3)", r.Stage, StageAuth)
	}
	if len(r.Gaps) != priorGapsLen {
		t.Errorf("Gaps len changed from %d to %d — must be preserved for completion_kind across parts",
			priorGapsLen, len(r.Gaps))
	}
	if r.PendingSplit {
		t.Error("PendingSplit not cleared — outer loop would never terminate")
	}
	if !r.HadWindowRoll {
		t.Error("HadWindowRoll cleared — completion_kind classification would lose part 1's partial signal across the boundary")
	}
	// resolvedAbove cleared — anchoring at a fresh seq and
	// committing it should advance frontier without "already
	// resolved" hits from the prior part.
	r.StartPart(50)
	r.NoteCommitted(50)
	if r.AccountedFrontierMediaSeq != 50 {
		t.Errorf("after BeginNewPart + StartPart(50) + NoteCommitted(50): frontier=%d, want 50",
			r.AccountedFrontierMediaSeq)
	}
}

func TestResumeState_ReanchorCurrentPartAfterEmptySplit_ReusesPartIndex(t *testing.T) {
	r := NewResumeState()
	r.CurrentPartIndex = 2
	r.StartPart(200)
	r.NoteRangeGap(200, 205, GapReasonRestartWindowRolled)
	r.SelectedQuality = "720"
	r.SelectedCodec = "h264"
	r.SegmentFormat = "ts"
	r.PartBytes = 1234
	r.PartDurationSeconds = 5
	r.PendingSplit = true
	r.HadWindowRoll = true
	r.EndListSeen = true // stale broadcast-ended marker
	r.SetStage(StageSegments)

	r.ReanchorCurrentPartAfterEmptySplit()

	if r.EndListSeen {
		t.Error("EndListSeen=true after ReanchorCurrentPartAfterEmptySplit; re-anchoring for a fresh window must clear the stale broadcast-ended marker")
	}

	if r.CurrentPartIndex != 2 {
		t.Fatalf("CurrentPartIndex=%d, want 2; empty skipped part must not consume a video_parts index", r.CurrentPartIndex)
	}
	if r.EmptySplitReanchors != 1 {
		t.Fatalf("EmptySplitReanchors=%d, want 1; skipped empty attempts must count toward split-loop cap", r.EmptySplitReanchors)
	}
	if r.PartStarted || r.PartStartMediaSequence != 0 || r.AccountedFrontierMediaSeq != 0 {
		t.Fatalf("part anchor not reset: started=%v start=%d frontier=%d",
			r.PartStarted, r.PartStartMediaSequence, r.AccountedFrontierMediaSeq)
	}
	if r.PendingSplit || r.PendingThresholdSplit {
		t.Fatalf("pending split not consumed: pending=%v threshold=%v", r.PendingSplit, r.PendingThresholdSplit)
	}
	if len(r.Gaps) != 0 {
		t.Fatalf("Gaps=%v, want empty; empty interval is not a persisted part", r.Gaps)
	}
	if r.SelectedQuality != "" || r.SelectedCodec != "" || r.SegmentFormat != "" {
		t.Fatalf("variant lock not cleared: quality=%q codec=%q format=%q", r.SelectedQuality, r.SelectedCodec, r.SegmentFormat)
	}
	if r.PartBytes != 0 || r.PartDurationSeconds != 0 {
		t.Fatalf("part accounting not reset: bytes=%d seconds=%v", r.PartBytes, r.PartDurationSeconds)
	}
	if r.Stage != StageAuth {
		t.Fatalf("Stage=%q, want %q", r.Stage, StageAuth)
	}
	if !r.HadWindowRoll {
		t.Fatal("HadWindowRoll was cleared; completion classification must survive the skipped empty interval")
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	restored, err := UnmarshalResumeState(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored.EmptySplitReanchors != 1 || restored.CurrentPartIndex != 2 {
		t.Fatalf("roundtrip counters: EmptySplitReanchors=%d CurrentPartIndex=%d, want 1/2",
			restored.EmptySplitReanchors, restored.CurrentPartIndex)
	}
}

// TestResumeState_HadWindowRoll_SurvivesPartBoundaryCrash:
// HadWindowRoll roundtrips through JSON and survives both
// BeginNewPart and the next part's StartPart. The completion_kind
// classifier reads the flag, not Gaps — which StartPart clears.
func TestResumeState_HadWindowRoll_SurvivesPartBoundaryCrash(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.NoteRangeGap(105, 110, GapReasonRestartWindowRolled)

	r.HadWindowRoll = true
	r.PendingSplit = true
	r.BeginNewPart()
	if !r.HadWindowRoll {
		t.Fatal("BeginNewPart cleared HadWindowRoll")
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r2, err := UnmarshalResumeState(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !r2.HadWindowRoll {
		t.Fatal("HadWindowRoll didn't roundtrip through JSON — partial signal lost on resume")
	}

	// Part 2's OnFirstPoll: StartPart(50) wipes Gaps (correctly —
	// those entries belonged to part 1). HadWindowRoll must NOT be
	r2.StartPart(50)
	if !r2.HadWindowRoll {
		t.Error("StartPart cleared HadWindowRoll")
	}
	if len(r2.Gaps) != 0 {
		t.Errorf("StartPart left Gaps non-empty: %v", r2.Gaps)
	}
}

// TestResumeState_ShouldOpenNextPart: cap-1 / cap / cap+1 boundary
// triple guards against a future >= → > swap.
func TestResumeState_ShouldOpenNextPart(t *testing.T) {
	cases := []struct {
		name             string
		pendingSplit     bool
		thresholdSplit   bool
		endListSeen      bool
		currentPartIndex int32
		emptyReanchors   int32
		maxDiscontinuity int32
		maxThreshold     int32
		wantContinue     bool
		wantErr          bool
	}{
		{"no split pending — terminate", false, false, false, 1, 0, 32, 1024, false, false},
		{"no split pending even past cap — terminate", false, false, false, 100, 0, 32, 1024, false, false},
		{"discontinuity split pending under cap — continue", true, false, false, 5, 0, 32, 1024, true, false},
		{"discontinuity split pending one below cap — continue", true, false, false, 31, 0, 32, 1024, true, false},
		{"discontinuity split pending exactly at cap — fail", true, false, false, 32, 0, 32, 1024, false, true},
		{"empty reanchors one below discontinuity cap — continue", true, false, false, 2, 29, 32, 1024, true, false},
		{"empty reanchors exactly at discontinuity cap — fail", true, false, false, 2, 30, 32, 1024, false, true},
		{"threshold split ignores discontinuity cap", true, true, false, 32, 0, 32, 1024, true, false},
		{"threshold split ignores empty discontinuity reanchors", true, true, false, 32, 900, 32, 1024, true, false},
		{"threshold split pending exactly at threshold cap — fail", true, true, false, 1024, 0, 32, 1024, false, true},
		{"ENDLIST is handled by run boundary logic, not cap helper", true, true, true, 1024, 0, 32, 1024, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResumeState()
			r.PendingSplit = tc.pendingSplit
			r.PendingThresholdSplit = tc.thresholdSplit
			r.EndListSeen = tc.endListSeen
			r.CurrentPartIndex = tc.currentPartIndex
			r.EmptySplitReanchors = tc.emptyReanchors
			got, err := r.ShouldOpenNextPart(tc.maxDiscontinuity, tc.maxThreshold)
			if got != tc.wantContinue {
				t.Errorf("continue = %v, want %v", got, tc.wantContinue)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestResumeState_PendingSplit_SurvivesCrashMidRunPart: the durable
// flag survives a JSON roundtrip and ShouldOpenNextPart consumes
// it. Without persistence the loop would exit after finalizing the
// in-flight part, leaving the next part unrun.
func TestResumeState_PendingSplit_SurvivesCrashMidRunPart(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	for seq := int64(100); seq <= 110; seq++ {
		r.NoteCommitted(seq)
	}
	r.SelectedQuality = "1080"
	r.SelectedCodec = "h264"
	r.SegmentFormat = "ts"
	r.SetStage(StageRemux)
	r.PendingSplit = true

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r2, err := UnmarshalResumeState(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !r2.PendingSplit {
		t.Fatal("PendingSplit didn't roundtrip — split intent lost on resume; part N+1 would never run")
	}
	cont, err := r2.ShouldOpenNextPart(MaxDiscontinuityPartsPerVideo, DefaultMaxThresholdPartsPerVideo)
	if err != nil {
		t.Fatalf("ShouldOpenNextPart: %v", err)
	}
	if !cont {
		t.Fatal("ShouldOpenNextPart returned false on resumed PendingSplit=true state — loop would exit without opening part N+1")
	}
}

// TestResumeState_BeginNewPart_NewVariantLowerSeq: regression guard
// against re-introducing a non-zero anchor placeholder, which would
// make `bootstrapped` stay true and the poller silently filter out
// the new variant's lower-seq segments.
func TestResumeState_BeginNewPart_NewVariantLowerSeq(t *testing.T) {
	r := NewResumeState()
	r.StartPart(1000)
	r.NoteCommitted(1000)
	r.NoteCommitted(1001)
	r.NoteCommitted(1002)

	r.BeginNewPart()

	// Both anchor fields zero — the next OnFirstPoll's StartPart(50)
	// will work even though 50 < 1002.
	if r.PartStarted || r.PartStartMediaSequence != 0 || r.AccountedFrontierMediaSeq != 0 {
		t.Fatalf("BeginNewPart left bootstrap state set: PartStarted=%v PartStart=%d Frontier=%d — fetchWithAuthRefresh would skip the re-anchor and the poller would filter out the new variant's segments",
			r.PartStarted, r.PartStartMediaSequence, r.AccountedFrontierMediaSeq)
	}

	// Re-anchor at a lower seq (the new variant's base).
	r.StartPart(50)
	r.NoteCommitted(50)
	r.NoteCommitted(51)
	if r.AccountedFrontierMediaSeq != 51 {
		t.Errorf("frontier after re-anchor=%d, want 51", r.AccountedFrontierMediaSeq)
	}
}
