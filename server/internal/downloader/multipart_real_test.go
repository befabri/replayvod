//go:build ffmpeg

// AC-CROSS-1: a mid-run variant drop splits the recording into two
// video_parts rows. Build tag `ffmpeg`; shared primitives in
// harness_test.go.

package downloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
)

// TestMultipart_VariantDropSplitsIntoTwoParts: variant A (TS 480p)
// records 3 segments, master drops A, B (fMP4 360p) takes over.
// Spec AC-CROSS-1 in .docs/spec/download-pipeline.md.
func TestMultipart_VariantDropSplitsIntoTwoParts(t *testing.T) {
	requireFFmpegHarness(t)

	edge := newTwitchEdge(t, defaultEdgeOpts())
	h := newHarnessService(t, edge.URL())
	defer h.svc.Shutdown()

	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_login",
		BroadcasterName:  "Harness Login",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_login",
		DisplayName:      "Harness Login",
		Quality:          repository.QualityHigh,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Drain in a goroutine — the emitter non-blocking-sends, so a
	// slow drain misses superseded events.
	progressCh := h.svc.Subscribe(jobID)
	if progressCh == nil {
		t.Fatal("Subscribe returned nil channel")
	}
	progressDone := make(chan struct{})
	var events []Progress
	go func() {
		events = drainProgress(progressCh)
		close(progressDone)
	}()

	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	videoID := job.VideoID

	video := waitForVideoStatus(t, h.repo, videoID, repository.VideoStatusDone, 60*time.Second)

	select {
	case <-progressDone:
	case <-time.After(5 * time.Second):
		t.Fatal("progress channel did not close 5s after video DONE")
	}

	parts, err := h.repo.ListVideoParts(context.Background(), videoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("video_parts count = %d, want 2 (parts: %+v)", len(parts), parts)
	}

	if parts[0].PartIndex != 1 || parts[1].PartIndex != 2 {
		t.Errorf("part_index ordering = [%d, %d], want [1, 2]", parts[0].PartIndex, parts[1].PartIndex)
	}
	if parts[0].Quality != "480" {
		t.Errorf("part 1 quality = %q, want \"480\"", parts[0].Quality)
	}
	if parts[1].Quality != "360" {
		t.Errorf("part 2 quality = %q, want \"360\"", parts[1].Quality)
	}
	if parts[0].SegmentFormat != "ts" {
		t.Errorf("part 1 segment_format = %q, want \"ts\"", parts[0].SegmentFormat)
	}
	if parts[1].SegmentFormat != "fmp4" {
		t.Errorf("part 2 segment_format = %q, want \"fmp4\"", parts[1].SegmentFormat)
	}

	if video.DurationSeconds == nil || video.SizeBytes == nil {
		t.Fatalf("video duration/size unset: dur=%v size=%v", video.DurationSeconds, video.SizeBytes)
	}
	wantDur := parts[0].DurationSeconds + parts[1].DurationSeconds
	wantSize := parts[0].SizeBytes + parts[1].SizeBytes
	if abs(*video.DurationSeconds-wantDur) > 0.001 {
		t.Errorf("video.duration_seconds = %f, want sum of parts %f", *video.DurationSeconds, wantDur)
	}
	if *video.SizeBytes != wantSize {
		t.Errorf("video.size_bytes = %d, want sum of parts %d", *video.SizeBytes, wantSize)
	}

	for _, p := range parts {
		path := filepath.Join(h.storageDir, "videos", p.Filename)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("storage file missing for part %d at %q: %v", p.PartIndex, path, err)
			continue
		}
		if info.Size() != p.SizeBytes {
			t.Errorf("part %d storage size = %d, video_parts.size_bytes = %d", p.PartIndex, info.Size(), p.SizeBytes)
		}
		if !strings.Contains(p.Filename, fmt.Sprintf("-part%02d", p.PartIndex)) {
			t.Errorf("part %d filename %q missing -part%02d suffix", p.PartIndex, p.Filename, p.PartIndex)
		}
	}

	maxPart := 0
	for _, ev := range events {
		if ev.PartIndex > maxPart {
			maxPart = ev.PartIndex
		}
	}
	if maxPart < 2 {
		t.Errorf("max PartIndex in progress events = %d, want ≥ 2", maxPart)
	}

	if !strings.HasSuffix(parts[0].Filename, "-part01.mp4") {
		t.Errorf("part 1 filename = %q, want suffix \"-part01.mp4\"", parts[0].Filename)
	}
}
