package downloader

import (
	"encoding/json"
	"slices"
	"testing"
)

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
