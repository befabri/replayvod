package downloader

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestResumePolicyCountsDistinctCommittedAndLostSequences(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.AccountedFrontierMediaSeq = 109
	r.CompletedAboveFrontier = []int64{110, 111, 111}
	r.Gaps = []Gap{
		{MediaSeq: 101, EndMediaSeq: 101, Reason: GapReasonAuth},
		{MediaSeq: 102, EndMediaSeq: 102, Reason: GapReasonStitchedAd},
		{MediaSeq: 103, EndMediaSeq: 104, Reason: GapReasonRestartWindowRolled},
		{MediaSeq: 105, EndMediaSeq: 106, Reason: GapReasonWindowRolled},
		{MediaSeq: 106, EndMediaSeq: 107, Reason: GapReasonFetchFailure},
		{MediaSeq: 110, EndMediaSeq: 110, Reason: GapReasonRefetchExpired},
		{MediaSeq: 80, EndMediaSeq: 90, Reason: GapReasonMalformed},
	}
	before := slices.Clone(r.Gaps)
	done, lost := r.policyCounts()
	if done != 5 || lost != 3 || !slices.Equal(r.Gaps, before) {
		t.Fatalf("policy seeds duplicate outcomes or changed checkpoint: done=%d lost=%d gaps=%+v", done, lost, r.Gaps)
	}
}

func TestResumePolicyCountsSurviveCheckpointAndResetPerPart(t *testing.T) {
	for _, nextPart := range []string{"discontinuity", "threshold"} {
		t.Run(nextPart, func(t *testing.T) {
			r := NewResumeState()
			r.StartPart(100)
			r.NoteCommittedSegment(100, 7, 1)
			r.NoteGap(101, GapReasonRefetchExpired)
			r.NoteGap(102, GapReasonAuth)
			r.NoteRangeGap(103, 110, GapReasonRestartWindowRolled)
			r.NoteRangeGap(111, 113, GapReasonWindowRolled)
			encoded, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			r, err = UnmarshalResumeState(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if done, lost := r.policyCounts(); done != 1 || lost != 4 {
				t.Fatalf("checkpoint lost checked losses or charged exempt restoration: done=%d lost=%d", done, lost)
			}
			if r.PartBytes != 7 || r.PartDurationSeconds != 1 {
				t.Fatal("policy seeding changed media metrics")
			}
			if nextPart == "discontinuity" {
				r.BeginNewPart()
			} else {
				r.ContinuePart()
			}
			if done, lost := r.policyCounts(); done != 0 || lost != 0 {
				t.Fatalf("next part inherited old policy counters: done=%d lost=%d", done, lost)
			}
			if nextPart == "discontinuity" {
				r.StartPart(100)
				if done, lost := r.policyCounts(); done != 0 || lost != 0 || len(r.Gaps) != 0 {
					t.Fatalf("overlapping rendition sequences inherited old losses: done=%d lost=%d gaps=%+v", done, lost, r.Gaps)
				}
			}
		})
	}
}

func TestWindowGapPreservesBufferedCommitBeforeCheckpointFold(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.NoteCommittedSegment(101, 7, 1)
	r.NoteCommittedSegment(103, 7, 1)
	r.NoteGap(104, GapReasonAuth)
	r.NoteRangeGap(100, 105, GapReasonWindowRolled)
	want := []Gap{
		{MediaSeq: 104, EndMediaSeq: 104, Reason: GapReasonAuth},
		{MediaSeq: 100, EndMediaSeq: 100, Reason: GapReasonWindowRolled},
		{MediaSeq: 102, EndMediaSeq: 102, Reason: GapReasonWindowRolled},
		{MediaSeq: 105, EndMediaSeq: 105, Reason: GapReasonWindowRolled},
	}
	if !slices.Equal(r.Gaps, want) || r.AccountedFrontierMediaSeq != 105 || r.PartBytes != 14 {
		t.Fatalf("window gap overwrote saved outcomes while folding: %+v", r)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	r, err = UnmarshalResumeState(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if done, lost := r.policyCounts(); done != 2 || lost != 3 {
		t.Fatalf("folded checkpoint counted saved bytes as window loss: done=%d lost=%d", done, lost)
	}
}

func TestResolvedSequencesExcludePendingAuthenticationAndEarlierParts(t *testing.T) {
	r := NewResumeState()
	r.StartPart(100)
	r.NoteCommittedSegment(100, 7, 1)
	r.NoteGap(102, GapReasonAuth)
	r.NoteGap(103, GapReasonStitchedAd)
	r.NoteRangeGap(104, 105, GapReasonWindowRolled)
	r.NoteCommittedSegment(106, 7, 1)
	if got := r.resolvedSeqs(); !slices.Equal(got, []int64{103, 104, 105, 106}) {
		t.Fatalf("acquisition exclusions include unresolved or old media: %v", got)
	}
	if !slices.Equal(r.AuthGapSeqs(), []int64{102}) {
		t.Fatal("excluding resolved media changed pending authentication")
	}
	r.BeginNewPart()
	if got := r.resolvedSeqs(); len(got) != 0 {
		t.Fatalf("new rendition inherited old resolved sequences: %v", got)
	}
}
