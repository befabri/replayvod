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

// TestResumeState_BeginNewPart pins the contract Phase 6f's part-
// split path depends on. Three properties are load-bearing:
//
//  1. PartStartMediaSequence + AccountedFrontierMediaSeq both zero,
//     so fetchWithAuthRefresh's `bootstrapped` check evaluates false
//     and the new variant's OnFirstPoll re-anchors via StartPart.
//     Carrying the prior part's last seq forward makes the poller
//     silently filter out the new variant's segments (Twitch
//     doesn't share seq counters across variants).
//  2. Variant lock cleared: SelectedQuality + SelectedCodec +
//     SegmentFormat empty so Stage 3 picks freely without the
//     mid-run-variant-change check tripping on iteration 1.
//  3. Gaps PRESERVED across the boundary. The video-level
//     completion_kind classifier looks for restart_window_rolled
//     across all parts; clearing here would silently drop earlier
//     parts' partial signal.
func TestResumeState_BeginNewPart(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.NoteCommitted(100)
	r.NoteCommitted(101)
	r.NoteCommitted(105) // out-of-order: lands in CompletedAboveFrontier
	r.NoteRangeGap(110, 112, GapReasonRestartWindowRolled)
	r.SelectedQuality = "1080"
	r.SelectedCodec = "h264"
	r.SegmentFormat = "ts"
	r.SetStage(StageSegments)
	// Split is in flight (set by run() before runPart). BeginNewPart
	// must clear it so the loop's `if !PendingSplit { break }` check
	// terminates after the next part completes (otherwise we'd loop
	// forever, opening empty parts).
	r.PendingSplit = true
	// HadWindowRoll persists across the boundary — completion_kind
	// classification at MarkVideoDone needs the cross-part signal.
	r.HadWindowRoll = true

	priorPart := r.CurrentPartIndex
	priorGapsLen := len(r.Gaps)

	r.BeginNewPart()

	if r.CurrentPartIndex != priorPart+1 {
		t.Errorf("CurrentPartIndex=%d, want %d", r.CurrentPartIndex, priorPart+1)
	}
	if r.PartStartMediaSequence != 0 {
		t.Errorf("PartStartMediaSequence=%d, want 0 (next OnFirstPoll re-anchors)", r.PartStartMediaSequence)
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
	// resolvedAbove cleared — verify by anchoring at a fresh seq
	// and confirming a NoteCommitted at the new part's first seq
	// advances cleanly without spurious "already resolved" hits.
	r.StartPart(50) // new variant's MEDIA-SEQUENCE base, lower than prior part's
	r.NoteCommitted(50)
	if r.AccountedFrontierMediaSeq != 50 {
		t.Errorf("after BeginNewPart + StartPart(50) + NoteCommitted(50): frontier=%d, want 50",
			r.AccountedFrontierMediaSeq)
	}
}

// TestResumeState_HadWindowRoll_SurvivesPartBoundaryCrash pins the
// contract that protects against a subtle crash-window regression:
//
// Sequence under test:
//  1. Part 1 records a restart_window_rolled gap (Gaps[0] set).
//  2. run() captures the signal into HadWindowRoll, calls
//     BeginNewPart, checkpoints. State on disk now has
//     HadWindowRoll=true AND part 1's Gaps still present (BeginNewPart
//     intentionally preserves Gaps).
//  3. Process crashes BEFORE part 2's first hls.Run poll.
//  4. Resume reads the JSON. HadWindowRoll=true survives.
//  5. Part 2's first OnFirstPoll fires, calls StartPart(base) which
//     clears Gaps (correctly — those entries were part 1's, not
//     part 2's).
//  6. The terminal completion_kind classifier reads HadWindowRoll,
//     not Gaps. Recording is correctly marked partial.
//
// If a future change accidentally clears HadWindowRoll in StartPart
// (the obvious "while we're at it" cleanup) or in BeginNewPart, the
// recording in this scenario would silently mark complete instead of
// partial. This test catches that.
func TestResumeState_HadWindowRoll_SurvivesPartBoundaryCrash(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.NoteRangeGap(105, 110, GapReasonRestartWindowRolled)

	// Mirror run()'s scan-and-set step before BeginNewPart.
	hadRoll := false
	for _, g := range r.Gaps {
		if g.Reason == GapReasonRestartWindowRolled {
			hadRoll = true
			break
		}
	}
	if !hadRoll {
		t.Fatal("setup: expected Gaps to contain a window-roll entry")
	}
	r.HadWindowRoll = hadRoll
	r.PendingSplit = true
	r.BeginNewPart()
	if !r.HadWindowRoll {
		t.Fatal("BeginNewPart cleared HadWindowRoll — partial signal lost across the boundary")
	}

	// Simulate the crash by serializing the in-memory state and
	// reconstructing it (what jobs.resume_state JSONB does).
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
	// touched; that's the field run() actually reads at MarkVideoDone.
	r2.StartPart(50)
	if !r2.HadWindowRoll {
		t.Error("StartPart cleared HadWindowRoll — recording would mark complete despite part 1's window roll")
	}
	if len(r2.Gaps) != 0 {
		t.Errorf("StartPart left Gaps non-empty: %v (expected wipe — those were part 1's gaps)", r2.Gaps)
	}
}

// TestResumeState_ShouldOpenNextPart pins the post-runPart decision
// the outer part loop relies on. Three properties matter:
//
//  1. PendingSplit=false short-circuits — the loop terminates
//     after a single-part recording or after the final part of a
//     multi-part recording where the last fetch returned cleanly.
//  2. PendingSplit=true under the cap continues — the next
//     iteration opens part N+1.
//  3. PendingSplit=true at or over the cap returns an error so
//     run()'s caller can fail the download with a meaningful
//     message; without this guard a pathological variant churn
//     would produce unbounded video_parts rows.
//
// Boundary checks (cap-1 / cap / cap+1) are explicit to catch a
// future >= → > swap.
func TestResumeState_ShouldOpenNextPart(t *testing.T) {
	cases := []struct {
		name             string
		pendingSplit     bool
		currentPartIndex int32
		maxParts         int32
		wantContinue     bool
		wantErr          bool
	}{
		{"no split pending — terminate", false, 1, 32, false, false},
		{"no split pending even past cap — terminate", false, 100, 32, false, false},
		{"split pending under cap — continue", true, 5, 32, true, false},
		{"split pending one below cap — continue", true, 31, 32, true, false},
		{"split pending exactly at cap — fail", true, 32, 32, false, true},
		{"split pending past cap — fail", true, 33, 32, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResumeState()
			r.PendingSplit = tc.pendingSplit
			r.CurrentPartIndex = tc.currentPartIndex
			got, err := r.ShouldOpenNextPart(tc.maxParts)
			if got != tc.wantContinue {
				t.Errorf("continue = %v, want %v", got, tc.wantContinue)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestResumeState_PendingSplit_SurvivesCrashMidRunPart pins the
// durable-flag contract that protects against the nastier crash
// window in Phase 6f's part-split path:
//
// Sequence under test:
//  1. fetchWithAuthRefresh signals a split and persists
//     PendingSplit=true via checkpointResume.
//  2. runPart starts Stage 5 → 10 of the just-finished part. The
//     stream is likely still live on the new variant; the
//     orchestrator wants to pick it up after this part finalizes.
//  3. Process crashes mid-Stage-6 (ffmpeg killed, OOM, deploy).
//  4. Resume reads the JSON. PendingSplit=true survives.
//  5. The resume-skip path runs Stages 6-10 (idempotent), the part
//     finalizes, and ShouldOpenNextPart returns true → BeginNewPart
//     fires → part N+1 opens.
//
// Without persistence, the local splitAndContinue bool from the
// first run-attempt would be lost in the crash and ShouldOpenNextPart
// would return false → loop exits → part N+1 never runs.
func TestResumeState_PendingSplit_SurvivesCrashMidRunPart(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	for seq := int64(100); seq <= 110; seq++ {
		r.NoteCommitted(seq)
	}
	r.SelectedQuality = "1080"
	r.SelectedCodec = "h264"
	r.SegmentFormat = "ts"
	r.SetStage(StageRemux) // crashed mid-runPart
	r.PendingSplit = true  // set by run() before runPart

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
	cont, err := r2.ShouldOpenNextPart(MaxPartsPerVideo)
	if err != nil {
		t.Fatalf("ShouldOpenNextPart: %v", err)
	}
	if !cont {
		t.Fatal("ShouldOpenNextPart returned false on resumed PendingSplit=true state — loop would exit without opening part N+1")
	}
}

// TestResumeState_BeginNewPart_NewVariantLowerSeq is the regression
// guard for the bug Phase 6f initially shipped with: BeginNewPart's
// predecessor (StartPart with prevLast+1) made bootstrapped evaluate
// true, so the new variant's OnFirstPoll never re-anchored, and
// hls.Run's startSeq filtered out every segment whose MediaSeq was
// below the carried-over threshold.
//
// This test simulates that scenario by checking that after
// BeginNewPart the per-part anchor is genuinely zero, ready for the
// new variant's first poll to set it. A future regression that puts
// non-zero placeholder values back in PartStartMediaSequence would
// re-introduce the silent-segment-drop bug.
func TestResumeState_BeginNewPart_NewVariantLowerSeq(t *testing.T) {
	r := NewResumeState()
	r.StartPart(1000)
	r.NoteCommitted(1000)
	r.NoteCommitted(1001)
	r.NoteCommitted(1002)

	r.BeginNewPart()

	// Both anchor fields zero — the next OnFirstPoll's StartPart(50)
	// will work even though 50 < 1002.
	if r.PartStartMediaSequence != 0 || r.AccountedFrontierMediaSeq != 0 {
		t.Fatalf("BeginNewPart left bootstrap state non-zero: PartStart=%d Frontier=%d — fetchWithAuthRefresh would skip the re-anchor and the poller would filter out the new variant's segments",
			r.PartStartMediaSequence, r.AccountedFrontierMediaSeq)
	}

	// Re-anchor at a lower seq (the new variant's base).
	r.StartPart(50)
	r.NoteCommitted(50)
	r.NoteCommitted(51)
	if r.AccountedFrontierMediaSeq != 51 {
		t.Errorf("frontier after re-anchor=%d, want 51", r.AccountedFrontierMediaSeq)
	}
}
