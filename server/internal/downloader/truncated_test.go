package downloader

import (
	"context"
	"errors"
	"github.com/befabri/replayvod/server/internal/repository"
	"testing"
)

func TestFailedRunTruncated(t *testing.T) {
	cases := []struct {
		name                          string
		partsKnown, hasPart, cutShort bool
		want                          bool
	}{
		{"nothing captured, cut short", true, false, true, false},
		{"nothing captured, ended cleanly", true, false, false, false},
		{"parts captured, cut short", true, true, true, true},
		{"parts captured, post-broadcast failure", true, true, false, false},
		{"parts unknown, cut short", false, false, true, true},
		{"parts unknown, ended cleanly", false, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := failedRunTruncated(tc.partsKnown, tc.hasPart, tc.cutShort); got != tc.want {
				t.Fatalf("failedRunTruncated(%v, %v, %v) = %v, want %v", tc.partsKnown, tc.hasPart, tc.cutShort, got, tc.want)
			}
		})
	}
}

type failedPartProbeRepo struct{ repository.Repository }

func (r failedPartProbeRepo) HasFinalizedVideoParts(context.Context, int64) (bool, error) {
	return false, errors.New("part lookup unavailable")
}

func TestFailDownload_PersistsTruncationOnlyForCapturedMedia(t *testing.T) {
	cases := []struct {
		name                                             string
		partSize                                         int64 // -1 means no row; zero means an unfinished part
		unknown, cancelled, ended, rolled, wantTruncated bool
		wantKind                                         string
	}{
		{name: "nothing captured", partSize: -1, wantKind: repository.CompletionKindComplete},
		{name: "cancelled before capture", partSize: -1, cancelled: true, wantKind: repository.CompletionKindCancelled},
		{name: "unfinished part", partSize: 0, wantKind: repository.CompletionKindComplete},
		{name: "saved part cut short", partSize: 100, wantTruncated: true, wantKind: repository.CompletionKindPartial},
		{name: "failure after broadcast", partSize: 100, ended: true, wantKind: repository.CompletionKindPartial},
		{name: "cancelled with media", partSize: 100, cancelled: true, ended: true, wantTruncated: true, wantKind: repository.CompletionKindCancelled},
		{name: "lost window", partSize: 100, ended: true, rolled: true, wantTruncated: true, wantKind: repository.CompletionKindPartial},
		{name: "unknown media cut short", partSize: -1, unknown: true, wantTruncated: true, wantKind: repository.CompletionKindComplete},
		{name: "unknown media after broadcast", partSize: -1, unknown: true, ended: true, wantKind: repository.CompletionKindComplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			ctx := t.Context()
			if _, err := s.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b", BroadcasterLogin: "b", BroadcasterName: "b"}); err != nil {
				t.Fatal(err)
			}
			id := seedRunningJobWithCorruptResume(t, ctx, s.repo, "job", "b", false)
			if tc.partSize >= 0 {
				p, err := s.repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: id, PartIndex: 1, Filename: "part.mp4", Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4})
				if err != nil {
					t.Fatal(err)
				}
				if tc.partSize > 0 {
					if err := s.repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{ID: p.ID, SizeBytes: tc.partSize, DurationSeconds: 1, EndMediaSeq: 1}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.unknown {
				s.repo = failedPartProbeRepo{s.repo}
			}
			d := &download{jobID: "job", videoID: id, userCancelled: tc.cancelled, resume: &ResumeState{EndListSeen: tc.ended, HadWindowRoll: tc.rolled}}
			s.failDownload(ctx, d, discardLog(), errors.New("capture failed"))
			video, err := s.repo.GetVideo(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if video.Status != repository.VideoStatusFailed || video.Truncated != tc.wantTruncated || video.CompletionKind != tc.wantKind {
				t.Fatalf("failure state = %s/%s truncated=%v, want FAILED/%s truncated=%v", video.Status, video.CompletionKind, video.Truncated, tc.wantKind, tc.wantTruncated)
			}
		})
	}
}
