package downloader

import "testing"

func withOneCommit() *ResumeState {
	r := NewResumeState()
	r.PartStarted = true
	r.PartStartMediaSequence = 100
	r.AccountedFrontierMediaSeq = 100
	return r
}

func TestPartThresholdAccountant_SealIfCrossed(t *testing.T) {
	t.Run("not crossed → no seal", func(t *testing.T) {
		var sealed []int64
		a := &partThresholdAccountant{resume: withOneCommit(), onSeal: func(b int64) { sealed = append(sealed, b) }}
		if a.sealIfCrossed(100, false) {
			t.Fatal("sealIfCrossed returned true for crossed=false")
		}
		if len(sealed) != 0 || a.resume.PendingSplit {
			t.Fatalf("seal fired on a non-crossing: sealed=%v pendingSplit=%v", sealed, a.resume.PendingSplit)
		}
	})

	t.Run("crossed with content → seals once", func(t *testing.T) {
		var sealed []int64
		a := &partThresholdAccountant{resume: withOneCommit(), onSeal: func(b int64) { sealed = append(sealed, b) }}
		if !a.sealIfCrossed(100, true) {
			t.Fatal("sealIfCrossed returned false for a crossing with content")
		}
		if len(sealed) != 1 || sealed[0] != 100 {
			t.Fatalf("onSeal calls = %v, want [100]", sealed)
		}
		if !a.resume.PendingSplit || !a.resume.PendingThresholdSplit {
			t.Fatalf("seal did not mark pending split: PendingSplit=%v PendingThresholdSplit=%v",
				a.resume.PendingSplit, a.resume.PendingThresholdSplit)
		}
	})

	t.Run("already pending → no second seal (idempotent)", func(t *testing.T) {
		var sealed []int64
		r := withOneCommit()
		r.PendingSplit = true
		a := &partThresholdAccountant{resume: r, onSeal: func(b int64) { sealed = append(sealed, b) }}
		if a.sealIfCrossed(100, true) {
			t.Fatal("sealIfCrossed re-sealed while a split was already pending")
		}
		if len(sealed) != 0 {
			t.Fatalf("onSeal fired despite pending split: %v", sealed)
		}
	})

	t.Run("crossed but no content → no seal (doom-loop guard)", func(t *testing.T) {
		var sealed []int64
		a := &partThresholdAccountant{resume: NewResumeState(), onSeal: func(b int64) { sealed = append(sealed, b) }}
		if a.sealIfCrossed(100, true) {
			t.Fatal("sealIfCrossed sealed a part with no content")
		}
		if len(sealed) != 0 || a.resume.PendingSplit {
			t.Fatalf("seal fired without content: sealed=%v pendingSplit=%v", sealed, a.resume.PendingSplit)
		}
	})
}

func TestPartThresholdAccountant_CommitSealsOnCeiling(t *testing.T) {
	t.Run("commit at byte ceiling seals", func(t *testing.T) {
		var sealed []int64
		r := NewResumeState()
		r.PartStarted = true
		r.PartStartMediaSequence = 100
		r.AccountedFrontierMediaSeq = 99
		a := &partThresholdAccountant{resume: r, maxBytes: 1000, onSeal: func(b int64) { sealed = append(sealed, b) }}

		a.commit(100, 1000, 2.0)
		if len(sealed) != 1 || sealed[0] != 100 {
			t.Fatalf("onSeal calls = %v, want [100]", sealed)
		}
		if !a.resume.PendingSplit {
			t.Fatal("commit at ceiling did not mark a pending split")
		}
	})

	t.Run("commit under ceiling does not seal", func(t *testing.T) {
		var sealed []int64
		r := NewResumeState()
		r.PartStarted = true
		r.PartStartMediaSequence = 100
		r.AccountedFrontierMediaSeq = 99
		a := &partThresholdAccountant{resume: r, maxBytes: 10_000, onSeal: func(b int64) { sealed = append(sealed, b) }}

		a.commit(100, 1000, 2.0)
		if len(sealed) != 0 || a.resume.PendingSplit {
			t.Fatalf("commit under ceiling sealed: sealed=%v pendingSplit=%v", sealed, a.resume.PendingSplit)
		}
	})
}

func TestPartThresholdAccountant_AuthGapNeverSeals(t *testing.T) {
	var sealed []int64
	r := withOneCommit()
	a := &partThresholdAccountant{resume: r, maxBytes: 1, maxSeconds: 1, onSeal: func(b int64) { sealed = append(sealed, b) }}

	a.authGap(101)

	if len(sealed) != 0 || r.PendingSplit {
		t.Fatalf("authGap sealed despite the ceiling-free contract: sealed=%v pendingSplit=%v", sealed, r.PendingSplit)
	}
	if len(r.Gaps) != 1 || r.Gaps[0].Reason != GapReasonAuth {
		t.Fatalf("authGap did not record an auth gap: %+v", r.Gaps)
	}
}

func TestPartThresholdAccountant_RecordRangeGapDoesNotSeal(t *testing.T) {
	var sealed []int64
	r := NewResumeState()
	r.PartStarted = true
	r.PartStartMediaSequence = 100
	r.AccountedFrontierMediaSeq = 99
	a := &partThresholdAccountant{resume: r, maxBytes: 1000, onSeal: func(b int64) { sealed = append(sealed, b) }}

	a.recordRangeGap(100, 110, GapReasonRestartWindowRolled)

	if len(sealed) != 0 || r.PendingSplit {
		t.Fatalf("recordRangeGap sealed on its own: sealed=%v pendingSplit=%v", sealed, r.PendingSplit)
	}
	if len(r.Gaps) == 0 {
		t.Fatal("recordRangeGap did not record the lost range")
	}
}
