package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

func TestRecoveryPlaylistAuthPreservesFirstSnapshotWindow(t *testing.T) {
	s, d, dir, resolutions, mediaRequests := authGapPolicyFixture(t, "recovery auth window")
	s.cfg.App.Download.Strict = true
	s.cfg.App.Download.MaxRestartGapSeconds = 1
	d.resume.StartPart(100)
	for seq := int64(100); seq < 110; seq++ {
		d.resume.NoteCommittedSegment(seq, 7, 1)
	}
	result, err := runAuthGapPolicyFixture(t, s, d, dir)
	if !errors.Is(err, ErrRestartGapExceeded) || !d.resume.PendingSplit || d.resume.PendingThresholdSplit {
		t.Fatalf("authorization before the first snapshot consumed recovery: result=%+v resume=%+v error=%v", result, d.resume, err)
	}
	want := []Gap{{MediaSeq: 110, EndMediaSeq: 114, Reason: GapReasonRestartWindowRolled}}
	if resolutions.Load() != 2 || mediaRequests.Load() != 0 || result.SegmentsDone != 10 || result.SegmentsGaps != 0 || d.resume.PartBytes != 70 || !slices.Equal(d.resume.Gaps, want) {
		t.Fatalf("restoration changed saved media or acquired beyond its split: result=%+v resume=%+v resolutions=%d requests=%d", result, d.resume, resolutions.Load(), mediaRequests.Load())
	}
}

func TestRecoveryPlaylistAuthRetainsCheckpointRefetch(t *testing.T) {
	for _, tc := range []struct {
		name, fixture string
		strict        bool
		wantDone      int64
		wantGaps      int64
		wantRequests  int32
	}{
		{"present", "recovery auth", true, 11, 0, 2},
		{"expired accepted", "recovery auth expired", false, 10, 1, 1},
		{"expired rejected", "recovery auth expired", true, 9, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, dir, resolutions, mediaRequests := authGapPolicyFixture(t, tc.fixture)
			s.cfg.App.Download.Strict = tc.strict
			s.cfg.App.Download.MaxGapRatio = 1
			d.resume.StartPart(100)
			for seq := int64(100); seq < 110; seq++ {
				if seq == 101 {
					d.resume.NoteGap(seq, GapReasonAuth)
				} else {
					d.resume.NoteCommittedSegment(seq, 7, 1)
				}
			}
			result, err := runAuthGapPolicyFixture(t, s, d, dir)
			if tc.name == "expired rejected" {
				var gap *hls.GapAbortError
				if !errors.As(err, &gap) || result.EndList || !slices.Equal(d.resume.AuthGapSeqs(), []int64{101}) {
					t.Fatalf("unavailable checkpoint retry bypassed strict policy: result=%+v resume=%+v error=%v", result, d.resume, err)
				}
			} else {
				if err != nil || !result.EndList || len(d.resume.AuthGapSeqs()) != 0 || d.resume.AccountedFrontierMediaSeq != 110 {
					t.Fatalf("renewal discarded the checkpoint retry: result=%+v resume=%+v error=%v", result, d.resume, err)
				}
				if tc.name == "present" {
					data, err := os.ReadFile(filepath.Join(dir, "101.ts"))
					if err != nil || string(data) != "segment" || len(d.resume.Gaps) != 0 {
						t.Fatalf("available checkpoint retry was not saved: gaps=%+v data=%q error=%v", d.resume.Gaps, data, err)
					}
				} else if !slices.Equal(d.resume.Gaps, []Gap{{MediaSeq: 101, EndMediaSeq: 101, Reason: GapReasonRefetchExpired}}) {
					t.Fatalf("expired checkpoint retry lacks permanent evidence: %+v", d.resume.Gaps)
				}
			}
			if resolutions.Load() != 2 || mediaRequests.Load() != tc.wantRequests || result.SegmentsDone != tc.wantDone || result.SegmentsGaps != tc.wantGaps || result.BytesWritten != int64(tc.wantRequests)*7 || d.resume.PartBytes != tc.wantDone*7 {
				t.Fatalf("renewal double-counted media or lost pending loss: result=%+v resume=%+v resolutions=%d requests=%d", result, d.resume, resolutions.Load(), mediaRequests.Load())
			}
			job, err := s.repo.GetJob(t.Context(), d.jobID)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := UnmarshalResumeState(job.ResumeState)
			if err != nil || !slices.Equal(saved.Gaps, d.resume.Gaps) || saved.PartBytes != d.resume.PartBytes {
				t.Fatalf("retry outcome did not survive checkpoint: saved=%+v error=%v", saved, err)
			}
		})
	}
}

func TestNewRenditionDoesNotRetryPreviousPartAuthorization(t *testing.T) {
	s, d, dir, resolutions, _ := authGapPolicyFixture(t, "recovery window")
	s.cfg.App.Download.Strict = true
	d.resume.StartPart(9)
	d.resume.NoteCommittedSegment(9, 7, 1)
	d.resume.NoteGap(10, GapReasonAuth)
	d.resume.BeginNewPart()
	result, err := runAuthGapPolicyFixture(t, s, d, dir)
	if err != nil || !result.EndList || result.SegmentsDone != 1 || result.SegmentsGaps != 0 || result.BytesWritten != 7 || resolutions.Load() != 1 {
		t.Fatalf("new rendition inherited an unrelated authorization sequence: result=%+v resolutions=%d error=%v", result, resolutions.Load(), err)
	}
	if d.resume.PartStartMediaSequence != 105 || d.resume.AccountedFrontierMediaSeq != 105 || d.resume.PartBytes != 7 || len(d.resume.Gaps) != 0 {
		t.Fatalf("new rendition retained previous-part accounting: %+v", d.resume)
	}
}
