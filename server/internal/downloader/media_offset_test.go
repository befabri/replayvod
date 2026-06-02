package downloader

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDownloadMediaOffsetUnavailableWhileAuthGapPending(t *testing.T) {
	resume := NewResumeState()
	resume.StartPart(1)
	resume.NoteCommittedSegment(1, 100, 5)
	resume.NoteGap(2, GapReasonAuth)
	resume.NoteCommittedSegment(3, 100, 5)

	d := &download{resume: resume}
	d.refreshMediaOffset()

	if got, ok := d.MediaOffsetSeconds(); ok {
		t.Fatalf("MediaOffsetSeconds() = %v/true with pending auth gap, want unavailable", got)
	}

	resume.NoteCommittedSegment(2, 100, 5)
	d.refreshMediaOffset()

	got, ok := d.MediaOffsetSeconds()
	if !ok {
		t.Fatal("MediaOffsetSeconds() unavailable after auth gap refetch succeeded")
	}
	if got != 15 {
		t.Fatalf("MediaOffsetSeconds() = %v, want 15", got)
	}
}

func TestServiceResolveMediaOffsetSecondsUsesActiveDownload(t *testing.T) {
	d := &download{videoID: 7, broadcasterID: "b-1"}
	d.setMediaOffset(33.5, true)
	s := &Service{active: map[string]*download{"job-1": d}}

	got, ok := s.ResolveMediaOffsetSeconds(context.Background(), "b-1", 7)
	if !ok {
		t.Fatal("ResolveMediaOffsetSeconds() unavailable for active exact download")
	}
	if got != 33.5 {
		t.Fatalf("ResolveMediaOffsetSeconds() = %v, want 33.5", got)
	}

	if got, ok := s.ResolveMediaOffsetSeconds(context.Background(), "other", 7); ok {
		t.Fatalf("ResolveMediaOffsetSeconds() = %v/true for broadcaster mismatch, want unavailable", got)
	}

	d.setMediaOffset(40, false)
	if got, ok := s.ResolveMediaOffsetSeconds(context.Background(), "b-1", 7); ok {
		t.Fatalf("ResolveMediaOffsetSeconds() = %v/true while offset is inexact, want unavailable", got)
	}
}

func TestListActiveProgressClearsStaleMediaOffsetWhenInexact(t *testing.T) {
	oldOffset := 12.5
	d := &download{
		jobID:     "job-1",
		startedAt: time.Now(),
	}
	d.setProgress(Progress{
		JobID:              "job-1",
		PartIndex:          2,
		Stage:              "segments",
		MediaOffsetSeconds: &oldOffset,
	})
	d.setMediaOffset(20, false)
	s := &Service{active: map[string]*download{"job-1": d}}

	progress := s.ListActiveProgress()
	if len(progress) != 1 {
		t.Fatalf("len(progress) = %d, want 1", len(progress))
	}
	if progress[0].MediaOffsetSeconds != nil {
		t.Fatalf("MediaOffsetSeconds = %v, want nil while offset is inexact", *progress[0].MediaOffsetSeconds)
	}
}

func TestDownloadMediaOffsetPairIsGuardedTogether(t *testing.T) {
	d := &download{}
	d.setMediaOffset(10, true)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 1_000; j++ {
				if got, ok := d.MediaOffsetSeconds(); ok && got != 10 && got != 30 {
					t.Errorf("MediaOffsetSeconds() = %v/true, want only exact values 10 or 30", got)
					return
				}
			}
		}()
	}
	close(start)
	for i := 0; i < 1_000; i++ {
		d.setMediaOffset(20, false)
		d.setMediaOffset(30, true)
		d.setMediaOffset(10, true)
	}
	wg.Wait()
}
