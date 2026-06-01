//go:build ffmpeg

// Size/duration part-split integration tests: a single healthy stream
// is cut into multiple contiguous video_parts by the MaxPartBytes /
// MaxPartSeconds ceiling, the disabled default stays single-file, and
// a crash mid-split resumes the continuation without re-fetching
// committed segments. Build tag `ffmpeg`; shared primitives in
// harness_test.go.

package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
)

// TestMultipart_DurationThresholdSplitsContiguously: one continuous
// variant crosses MaxPartSeconds and is cut into multiple parts. The
// cut is clean — part N+1 starts at exactly part N's last media
// sequence + 1 (no gap, no duplicate), the same variant carries
// across the boundary, the aggregate duration/size equals the sum of
// parts, and completion is "complete" (a size/duration split is not a
// loss). 1s segments + a 3s ceiling cut after 3 segments.
func TestMultipart_DurationThresholdSplitsContiguously(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 5
	opts.windowA = 5 // serve-all window: the continuation never window-rolls
	opts.dropAfterServed = 0
	opts.aEndlist = 5
	opts.fmp4Count = 0 // single variant — the split is purely the duration ceiling
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	defer h.svc.Shutdown()
	// Set before Start so the run goroutine reads it race-free. 1s
	// segments; a 3s ceiling splits after the 3rd commit.
	h.svc.cfg.App.Download.MaxPartSeconds = 3

	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_partsplit",
		BroadcasterName:  "Harness PartSplit",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_partsplit",
		DisplayName:      "Harness PartSplit",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	video := waitForVideoStatus(t, h.repo, job.VideoID, repository.VideoStatusDone, 60*time.Second)

	parts, err := h.repo.ListVideoParts(context.Background(), job.VideoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("video_parts count = %d, want 2 (3s ceiling over 5s of segments should split at seq %d; parts: %+v)",
			len(parts), opts.baseSeqA+2, parts)
	}
	assertPartRange(t, parts[0], int64(opts.baseSeqA), int64(opts.baseSeqA+2))
	assertPartRange(t, parts[1], int64(opts.baseSeqA+3), int64(opts.baseSeqA+opts.tsCount-1))

	assertContiguousCoverage(t, parts, int64(opts.baseSeqA), int64(opts.baseSeqA+opts.tsCount-1))

	if video.DurationSeconds == nil || video.SizeBytes == nil {
		t.Fatalf("video duration/size unset: dur=%v size=%v", video.DurationSeconds, video.SizeBytes)
	}
	var wantDur float64
	var wantSize int64
	for _, p := range parts {
		wantDur += p.DurationSeconds
		wantSize += p.SizeBytes
	}
	if abs(*video.DurationSeconds-wantDur) > 0.001 {
		t.Errorf("video.duration_seconds = %f, want sum of parts %f", *video.DurationSeconds, wantDur)
	}
	if *video.SizeBytes != wantSize {
		t.Errorf("video.size_bytes = %d, want sum of parts %d", *video.SizeBytes, wantSize)
	}

	// A size/duration split is a clean cut, not a loss — never partial.
	if video.CompletionKind != repository.CompletionKindComplete {
		t.Errorf("completion_kind = %q, want %q (threshold split is a clean cut)",
			video.CompletionKind, repository.CompletionKindComplete)
	}
}

// TestMultipart_ByteThresholdSplitsContiguously is the size-ceiling twin of
// the duration test above, closing the gap where MaxPartBytes was only
// exercised by threshold accounting helpers and never end to end. It drives
// the OnEvent PartBytes accumulator through a real recording: a
// 1-byte ceiling is crossed by every committed segment, so each segment must
// become its own part. That exact part range assertion catches out-of-order
// worker completion incorrectly sealing the boundary at a later buffered
// sequence.
func TestMultipart_ByteThresholdSplitsContiguously(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 5
	opts.windowA = 5 // serve-all window: the continuation never window-rolls
	opts.dropAfterServed = 0
	opts.aEndlist = 5
	opts.fmp4Count = 0 // single variant — the split is purely the size ceiling
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	defer h.svc.Shutdown()
	// Set before Start so the run goroutine reads it race-free. A 1-byte ceiling
	// is exceeded by every (multi-KB) segment, so the part is cut on each
	// committed segment boundary.
	h.svc.cfg.App.Download.MaxPartBytes = 1

	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_bytesplit",
		BroadcasterName:  "Harness ByteSplit",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_bytesplit",
		DisplayName:      "Harness ByteSplit",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	video := waitForVideoStatus(t, h.repo, job.VideoID, repository.VideoStatusDone, 60*time.Second)

	parts, err := h.repo.ListVideoParts(context.Background(), job.VideoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != opts.tsCount {
		t.Fatalf("video_parts count = %d, want %d (1-byte ceiling must cut every segment; parts: %+v)",
			len(parts), opts.tsCount, parts)
	}
	for i, p := range parts {
		wantSeq := int64(opts.baseSeqA + i)
		assertPartRange(t, p, wantSeq, wantSeq)
	}

	assertContiguousCoverage(t, parts, int64(opts.baseSeqA), int64(opts.baseSeqA+opts.tsCount-1))

	if video.DurationSeconds == nil || video.SizeBytes == nil {
		t.Fatalf("video duration/size unset: dur=%v size=%v", video.DurationSeconds, video.SizeBytes)
	}
	var wantDur float64
	var wantSize int64
	for _, p := range parts {
		wantDur += p.DurationSeconds
		wantSize += p.SizeBytes
	}
	if abs(*video.DurationSeconds-wantDur) > 0.001 {
		t.Errorf("video.duration_seconds = %f, want sum of parts %f", *video.DurationSeconds, wantDur)
	}
	if *video.SizeBytes != wantSize {
		t.Errorf("video.size_bytes = %d, want sum of parts %d", *video.SizeBytes, wantSize)
	}
	if video.CompletionKind != repository.CompletionKindComplete {
		t.Errorf("completion_kind = %q, want %q (size split is a clean cut)",
			video.CompletionKind, repository.CompletionKindComplete)
	}
}

// TestMultipart_ThresholdDisabledStaysSinglePart: with both ceilings
// at 0 (the default) an identical stream records as exactly one part —
// the feature is inert unless configured, preserving today's
// single-file behavior.
func TestMultipart_ThresholdDisabledStaysSinglePart(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 5
	opts.windowA = 5
	opts.dropAfterServed = 0
	opts.aEndlist = 5
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	defer h.svc.Shutdown()
	// MaxPartBytes / MaxPartSeconds left at 0 (disabled).

	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_nosplit",
		BroadcasterName:  "Harness NoSplit",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_nosplit",
		DisplayName:      "Harness NoSplit",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	video := waitForVideoStatus(t, h.repo, job.VideoID, repository.VideoStatusDone, 60*time.Second)

	// A recording that ran to the playlist's EXT-X-ENDLIST captured the whole
	// broadcast, so it must NOT be flagged truncated. This is the end-to-end
	// regression guard for the EndListSeen wiring (poller -> JobResult.EndList
	// -> resume.EndListSeen -> truncated): before it was wired, EndListSeen was
	// never set and every completed recording reported truncated=true.
	if video.Truncated {
		t.Error("video.Truncated=true after a clean ENDLIST recording, want false")
	}
	if video.CompletionKind != repository.CompletionKindComplete {
		t.Errorf("completion_kind = %q, want %q", video.CompletionKind, repository.CompletionKindComplete)
	}

	parts, err := h.repo.ListVideoParts(context.Background(), job.VideoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("threshold disabled: video_parts count = %d, want 1 (single-file behavior; parts: %+v)",
			len(parts), parts)
	}
	if parts[0].StartMediaSeq != int64(opts.baseSeqA) {
		t.Errorf("part 1 StartMediaSeq = %d, want %d", parts[0].StartMediaSeq, opts.baseSeqA)
	}
	if parts[0].EndMediaSeq == nil || *parts[0].EndMediaSeq != int64(opts.baseSeqA+opts.tsCount-1) {
		t.Errorf("part 1 EndMediaSeq = %v, want %d (whole stream in one part)",
			parts[0].EndMediaSeq, opts.baseSeqA+opts.tsCount-1)
	}
}

// TestResume_PendingThresholdSplit_ContinuesContiguously: a process
// whose checkpoint captured a size/duration split decision
// (PendingSplit + PendingThresholdSplit) before crashing mid-runPart
// must resume the next part as a CONTINUATION — part 2 starts exactly
// one past part 1's end with no re-anchor, no overlap, and no re-fetch
// of part 1's committed segments. This is the size/duration analogue
// of TestResume_PendingSplitTrue_OpensPartNPlusOne, and the strict
// (non-overlapping) contiguity is what distinguishes ContinuePart from
// BeginNewPart's re-anchor.
func TestResume_PendingThresholdSplit_ContinuesContiguously(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 12
	opts.windowA = 12 // serve-all: the continuation never window-rolls
	opts.dropAfterServed = 0
	opts.aEndlist = 12
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_threshsplit",
		BroadcasterName:  "Harness ThreshSplit",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_threshsplit",
		DisplayName:      "Harness ThreshSplit",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	videoID := job.VideoID

	waitForJobResumeState(t, h.repo, jobID, func(s *ResumeState) bool {
		return s.AccountedFrontierMediaSeq >= int64(opts.baseSeqA+3)
	}, 30*time.Second)

	h.svc.Shutdown()

	// Simulate the pre-runPart crash window: the checkpoint captured a
	// size/duration split decision while still in SEGMENTS. Resume must
	// treat that pending split as "fetch complete for this part" instead
	// of re-entering HLS and appending more stream data into part 1.
	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job pre-mutate: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal pre-mutate: %v", err)
	}
	boundary := int64(opts.baseSeqA + 3)
	state.SetStage(StageSegments)
	state.PartStarted = true
	state.PartStartMediaSequence = int64(opts.baseSeqA)
	state.AccountedFrontierMediaSeq = boundary
	state.CompletedAboveFrontier = nil
	state.CompletedAboveFrontierAccounting = nil
	state.Gaps = nil
	state.SegmentFormat = "ts"
	state.PendingSplit = true
	state.PendingThresholdSplit = true
	state.SealThresholdSplitBoundary(boundary)
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal mutate: %v", err)
	}
	if err := h.repo.UpdateJobResumeState(context.Background(), jobID, data); err != nil {
		t.Fatalf("write mutated resume state: %v", err)
	}

	resumed := resumeOver(t, h, edge.URL())
	defer resumed.svc.Shutdown()
	if err := resumed.svc.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	video := waitForVideoStatus(t, resumed.repo, videoID, repository.VideoStatusDone, 60*time.Second)

	parts, err := resumed.repo.ListVideoParts(context.Background(), videoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("video_parts count = %d, want 2 (threshold continuation; parts: %+v)", len(parts), parts)
	}

	// Strict continuation: part 1 is sealed at the checkpointed
	// threshold boundary, and part 2 begins at exactly part 1 end + 1.
	// BeginNewPart's re-anchor would regress part 2's start below this
	// (overlap = re-fetch); ContinuePart must not.
	if parts[0].EndMediaSeq == nil {
		t.Fatalf("part 1 EndMediaSeq nil — finalization didn't land")
	}
	if *parts[0].EndMediaSeq != boundary {
		t.Errorf("part 1 EndMediaSeq = %d, want checkpointed split boundary %d", *parts[0].EndMediaSeq, boundary)
	}
	if parts[1].StartMediaSeq != *parts[0].EndMediaSeq+1 {
		t.Errorf("part 2 StartMediaSeq = %d, want %d (part 1 EndMediaSeq+1) — non-contiguous (re-anchor or re-fetch)",
			parts[1].StartMediaSeq, *parts[0].EndMediaSeq+1)
	}
	// Same variant across the cut — the stream never changed.
	if parts[0].Quality != parts[1].Quality || parts[0].SegmentFormat != parts[1].SegmentFormat {
		t.Errorf("variant changed across threshold split: part 1 (%s/%s) -> part 2 (%s/%s)",
			parts[0].Quality, parts[0].SegmentFormat, parts[1].Quality, parts[1].SegmentFormat)
	}
	// Clean cut, not a loss.
	if video.CompletionKind != repository.CompletionKindComplete {
		t.Errorf("completion_kind = %q, want %q", video.CompletionKind, repository.CompletionKindComplete)
	}

	// The split flags must be consumed (else ShouldOpenNextPart would
	// loop until the discontinuity split cap).
	job, err = resumed.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job post-resume: %v", err)
	}
	final, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal final: %v", err)
	}
	if final.PendingSplit || final.PendingThresholdSplit {
		t.Errorf("split flags still set after completion: PendingSplit=%v PendingThresholdSplit=%v",
			final.PendingSplit, final.PendingThresholdSplit)
	}
}

// assertContiguousCoverage verifies the parts tile [wantStart, wantEnd]
// with no gap and no duplicate: part 1 starts at wantStart, each next
// part starts one past the prior's end, the last ends at wantEnd, and
// every adjacent pair keeps the same variant (a size/duration split
// never changes the stream).
func assertContiguousCoverage(t *testing.T, parts []repository.VideoPart, wantStart, wantEnd int64) {
	t.Helper()
	if parts[0].StartMediaSeq != wantStart {
		t.Errorf("part 1 StartMediaSeq = %d, want %d", parts[0].StartMediaSeq, wantStart)
	}
	for i := 0; i < len(parts)-1; i++ {
		if parts[i].EndMediaSeq == nil {
			t.Fatalf("part %d EndMediaSeq nil", parts[i].PartIndex)
		}
		if parts[i+1].StartMediaSeq != *parts[i].EndMediaSeq+1 {
			t.Errorf("part %d StartMediaSeq = %d, want %d (part %d EndMediaSeq+1) — gap or duplicate",
				parts[i+1].PartIndex, parts[i+1].StartMediaSeq, *parts[i].EndMediaSeq+1, parts[i].PartIndex)
		}
		if parts[i].Quality != parts[i+1].Quality || parts[i].SegmentFormat != parts[i+1].SegmentFormat {
			t.Errorf("variant changed across split: part %d (%s/%s) -> part %d (%s/%s)",
				parts[i].PartIndex, parts[i].Quality, parts[i].SegmentFormat,
				parts[i+1].PartIndex, parts[i+1].Quality, parts[i+1].SegmentFormat)
		}
	}
	last := parts[len(parts)-1]
	if last.EndMediaSeq == nil || *last.EndMediaSeq != wantEnd {
		t.Errorf("last part EndMediaSeq = %v, want %d (full coverage, no dropped tail)", last.EndMediaSeq, wantEnd)
	}
}

func assertPartRange(t *testing.T, part repository.VideoPart, wantStart, wantEnd int64) {
	t.Helper()
	if part.StartMediaSeq != wantStart {
		t.Errorf("part %d StartMediaSeq = %d, want %d", part.PartIndex, part.StartMediaSeq, wantStart)
	}
	if part.EndMediaSeq == nil {
		t.Fatalf("part %d EndMediaSeq nil, want %d", part.PartIndex, wantEnd)
	}
	if *part.EndMediaSeq != wantEnd {
		t.Errorf("part %d EndMediaSeq = %d, want %d", part.PartIndex, *part.EndMediaSeq, wantEnd)
	}
}

// TestResume_WindowRollFoldSealsThresholdMidPart exercises the
// window-roll-fold path end to end through real ffmpeg remux. A crash
// leaves segments committed ABOVE an unfilled hole (concurrent workers
// finished 104,105 before 103); on resume the CDN window has rolled
// past the hole, so the first poll's OnWindowRoll fills 103 in one
// shot, folding the buffered 104+105 over a 2s ceiling. The fold must
// SEAL the part at 105 (NoteRangeGapUntilThreshold → maybeForcePartThreshold
// → ContinuePart), not silently overshoot.
//
// Discriminator: with the threshold-aware range-gap wiring the part
// seals at 105 (the second folded segment crosses 2s). Without it the
// range gap advances with the ceiling disabled, the part keeps growing,
// and the seal only lands later via the next live commit — so part 1's
// EndMediaSeq would be 106+, not 105. The whole stream is still tiled
// contiguously with no dropped/duplicated boundary.
func TestResume_WindowRollFoldSealsThresholdMidPart(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 10 // seqs 100..109
	opts.windowA = 3
	opts.dropAfterServed = 0
	opts.aEndlist = 10
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_wrfold",
		BroadcasterName:  "Harness WindowRollFold",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_wrfold",
		DisplayName:      "Harness WindowRollFold",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	videoID := job.VideoID

	// Let the first process commit through seq 105 (segment files
	// 100.ts..105.ts land on disk in part01/segments), then crash.
	waitForJobResumeState(t, h.repo, jobID, func(s *ResumeState) bool {
		return s.AccountedFrontierMediaSeq >= int64(opts.baseSeqA+5)
	}, 30*time.Second)
	h.svc.Shutdown()

	segDir := filepath.Join(h.scratchDir, jobID, "part01", "segments")
	sz104 := statSize(t, filepath.Join(segDir, "104.ts"))
	sz105 := statSize(t, filepath.Join(segDir, "105.ts"))
	// Punch the hole: seq 103 was never durably the frontier; drop its
	// file so the resume can't just re-read it, mirroring a seq that
	// only ever existed in the rolled-off window.
	if err := os.Remove(filepath.Join(segDir, "103.ts")); err != nil {
		t.Fatalf("remove hole segment 103.ts: %v", err)
	}

	// Rewrite the checkpoint into the crash shape: part 1 anchored at
	// 100, frontier stuck at 102 (103 unfilled), 104 and 105 committed
	// above the hole with their durable byte/duration accounting, and
	// the per-part accumulators reset so the ceiling counts from the
	// fold. SegmentFormat / Selected* are kept from the live run.
	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job pre-mutate: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal pre-mutate: %v", err)
	}
	state.SetStage(StageSegments)
	state.CurrentPartIndex = 1
	state.PartStarted = true
	state.PartStartMediaSequence = int64(opts.baseSeqA)
	state.AccountedFrontierMediaSeq = int64(opts.baseSeqA + 2) // 102; hole at 103
	state.Gaps = nil
	state.PartBytes = 0
	state.PartDurationSeconds = 0
	state.PendingSplit = false
	state.PendingThresholdSplit = false
	state.PendingSplitBoundarySet = false
	if state.SegmentFormat == "" {
		state.SegmentFormat = "ts"
	}
	// Buffered commits above the hole. completedAccounting (the
	// hot-path map MarshalJSON serializes from) must be populated, not
	// just the exported slice — see ResumeState.MarshalJSON.
	state.CompletedAboveFrontier = []int64{104, 105}
	state.completedAccounting = map[int64]CompletedSegmentAccounting{
		104: {MediaSeq: 104, Bytes: sz104, DurationSeconds: 1.0},
		105: {MediaSeq: 105, Bytes: sz105, DurationSeconds: 1.0},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal mutate: %v", err)
	}
	if err := h.repo.UpdateJobResumeState(context.Background(), jobID, data); err != nil {
		t.Fatalf("write mutated resume state: %v", err)
	}

	// Roll the CDN window forward to base 104 so the first resume poll
	// reports media-sequence base 104 (> frontier+1=103): seq 103 is
	// gone, 104/105 are still in-window. aCursor=6 → first poll bumps
	// to 7 → window [104,105,106].
	edge.mu.Lock()
	edge.aCursor = 6
	edge.aDropped = false
	edge.pendingJump = 0
	edge.mu.Unlock()

	resumed := resumeOver(t, h, edge.URL())
	defer resumed.svc.Shutdown()
	resumed.svc.cfg.App.Download.MaxPartSeconds = 2 // 104+105 = 2.0s crosses; one 1.0s segment does not
	if err := resumed.svc.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	video := waitForVideoStatus(t, resumed.repo, videoID, repository.VideoStatusDone, 60*time.Second)

	parts, err := resumed.repo.ListVideoParts(context.Background(), videoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) < 2 {
		t.Fatalf("video_parts count = %d, want >= 2 (the window-roll fold must split the part; parts: %+v)",
			len(parts), parts)
	}

	// The load-bearing assertion: the fold sealed part 1 at exactly 105
	// (the second buffered segment crossing 2.0s). Without the
	// threshold-aware range-gap path the seal slips to the next live
	// commit and part 1's EndMediaSeq is 106+.
	if parts[0].EndMediaSeq == nil {
		t.Fatalf("part 1 EndMediaSeq nil — finalization didn't land")
	}
	if *parts[0].EndMediaSeq != int64(opts.baseSeqA+5) {
		t.Fatalf("part 1 EndMediaSeq = %d, want %d (window-roll fold must seal at the segment that crosses the ceiling, not overshoot)",
			*parts[0].EndMediaSeq, opts.baseSeqA+5)
	}
	if parts[1].StartMediaSeq != *parts[0].EndMediaSeq+1 {
		t.Errorf("part 2 StartMediaSeq = %d, want %d (contiguous continuation past the sealed boundary)",
			parts[1].StartMediaSeq, *parts[0].EndMediaSeq+1)
	}

	// Whole stream tiled contiguously across however many parts the
	// continuation's own ceiling produced — no gap or duplicate at any
	// part boundary.
	assertContiguousCoverage(t, parts, int64(opts.baseSeqA), int64(opts.baseSeqA+opts.tsCount-1))

	// A window roll dropped seq 103, so the recording is truncated/partial.
	if video.CompletionKind != repository.CompletionKindPartial {
		t.Errorf("completion_kind = %q, want %q (a window roll lost a segment)",
			video.CompletionKind, repository.CompletionKindPartial)
	}
}

func statSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

func copySegment(t *testing.T, srcDir, dstDir, name string) int64 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(srcDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, name), data, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return int64(len(data))
}

// TestResume_WindowRollFoldSealsThresholdContinuationPart is the
// priorParts>0 twin of TestResume_WindowRollFoldSealsThresholdMidPart,
// covering Issue B's run-loop routing: a sealed threshold split with
// this-run SegmentsDone==0 must finalize through prune+runPart even when
// a prior part already exists, NOT be mis-routed by shouldSkipEmptySplitPart
// into reanchorCurrentPartAfterEmptySplit (which would abandon the on-disk
// segments and reuse the part index).
//
// Shape: a finalized part 1 (manually written so priorParts==1), then part
// 2 resumes with two segments (104,105) committed above an unfilled hole at
// 103. The first poll's window roll folds 104+105 over a 2s ceiling and
// seals part 2 at 105 with no new commit this run.
//
// Discriminator: part 2 (index 2) seals at EndMediaSeq 105. Without the
// Issue B fix shouldSkipEmptySplitPart reanchors, so the index-2 row is
// rebuilt from the live window and its EndMediaSeq is not 105.
func TestResume_WindowRollFoldSealsThresholdContinuationPart(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 12 // seqs 100..111
	opts.windowA = 3
	opts.dropAfterServed = 0
	opts.aEndlist = 12
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_wrfold2",
		BroadcasterName:  "Harness WindowRollFold2",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_wrfold2",
		DisplayName:      "Harness WindowRollFold2",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	videoID := job.VideoID

	waitForJobResumeState(t, h.repo, jobID, func(s *ResumeState) bool {
		return s.AccountedFrontierMediaSeq >= int64(opts.baseSeqA+5)
	}, 30*time.Second)
	h.svc.Shutdown()

	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job pre-mutate: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal pre-mutate: %v", err)
	}

	// Part 2's segment dir holds only the two buffered commits above the
	// hole; 100..102 belong to the (manually finalized) part 1.
	part01seg := filepath.Join(h.scratchDir, jobID, "part01", "segments")
	part02seg := filepath.Join(h.scratchDir, jobID, "part02", "segments")
	if err := os.MkdirAll(part02seg, 0o755); err != nil {
		t.Fatalf("mkdir part02 segments: %v", err)
	}
	sz104 := copySegment(t, part01seg, part02seg, "104.ts")
	sz105 := copySegment(t, part01seg, part02seg, "105.ts")

	// A real finalized part 1 (index 1 < CurrentPartIndex 2 → priorParts==1).
	// The first process's shutdown can race runPart far enough to have
	// already inserted the part-1 row, so get-or-create rather than assume.
	p1, err := h.repo.GetVideoPartByIndex(context.Background(), videoID, 1)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && p1 == nil) {
		p1, err = h.repo.CreateVideoPart(context.Background(), &repository.VideoPartInput{
			VideoID:       videoID,
			PartIndex:     1,
			Filename:      "harness_wrfold2-part01.mp4",
			Quality:       state.SelectedQuality,
			FPS:           state.SelectedFPS,
			Codec:         state.SelectedCodec,
			SegmentFormat: "ts",
			StartMediaSeq: int64(opts.baseSeqA),
		})
	}
	if err != nil {
		t.Fatalf("get-or-create part 1: %v", err)
	}
	if err := h.repo.FinalizeVideoPart(context.Background(), &repository.VideoPartFinalize{
		ID:              p1.ID,
		DurationSeconds: 3.0,
		SizeBytes:       sz104 + sz105,
		EndMediaSeq:     int64(opts.baseSeqA + 2), // part 1 = 100..102
	}); err != nil {
		t.Fatalf("finalize part 1: %v", err)
	}

	// Part 2 crash shape: anchored at 103, frontier stuck at 102 (hole at
	// 103), 104/105 committed above with their durable accounting.
	state.SetStage(StageSegments)
	state.CurrentPartIndex = 2
	state.PartStarted = true
	state.PartStartMediaSequence = int64(opts.baseSeqA + 3) // 103
	state.AccountedFrontierMediaSeq = int64(opts.baseSeqA + 2)
	state.Gaps = nil
	state.PartBytes = 0
	state.PartDurationSeconds = 0
	state.PendingSplit = false
	state.PendingThresholdSplit = false
	state.PendingSplitBoundarySet = false
	if state.SegmentFormat == "" {
		state.SegmentFormat = "ts"
	}
	state.CompletedAboveFrontier = []int64{104, 105}
	state.completedAccounting = map[int64]CompletedSegmentAccounting{
		104: {MediaSeq: 104, Bytes: sz104, DurationSeconds: 1.0},
		105: {MediaSeq: 105, Bytes: sz105, DurationSeconds: 1.0},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal mutate: %v", err)
	}
	if err := h.repo.UpdateJobResumeState(context.Background(), jobID, data); err != nil {
		t.Fatalf("write mutated resume state: %v", err)
	}

	// Roll the window to base 104 (past the hole at 103).
	edge.mu.Lock()
	edge.aCursor = 6
	edge.aDropped = false
	edge.pendingJump = 0
	edge.mu.Unlock()

	resumed := resumeOver(t, h, edge.URL())
	defer resumed.svc.Shutdown()
	resumed.svc.cfg.App.Download.MaxPartSeconds = 2
	if err := resumed.svc.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	video := waitForVideoStatus(t, resumed.repo, videoID, repository.VideoStatusDone, 60*time.Second)

	parts, err := resumed.repo.ListVideoParts(context.Background(), videoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) < 3 {
		t.Fatalf("video_parts count = %d, want >= 3 (part 1 + sealed part 2 + continuation; parts: %+v)",
			len(parts), parts)
	}

	var p2 *repository.VideoPart
	for i := range parts {
		if parts[i].PartIndex == 2 {
			p2 = &parts[i]
		}
	}
	if p2 == nil {
		t.Fatalf("no part_index 2 row; parts: %+v", parts)
	}
	// The load-bearing assertion (Issue B): part 2 finalized at the sealed
	// fold boundary 105 with its on-disk segments, instead of being
	// reanchored/abandoned.
	if p2.EndMediaSeq == nil {
		t.Fatalf("part 2 EndMediaSeq nil — finalization didn't land")
	}
	if *p2.EndMediaSeq != int64(opts.baseSeqA+5) {
		t.Fatalf("part 2 EndMediaSeq = %d, want %d — a sealed threshold split with priorParts>0 must finalize, not reanchor",
			*p2.EndMediaSeq, opts.baseSeqA+5)
	}
	if p2.StartMediaSeq != int64(opts.baseSeqA+3) {
		t.Errorf("part 2 StartMediaSeq = %d, want %d", p2.StartMediaSeq, opts.baseSeqA+3)
	}

	if video.CompletionKind != repository.CompletionKindPartial {
		t.Errorf("completion_kind = %q, want %q (a window roll lost seq 103)",
			video.CompletionKind, repository.CompletionKindPartial)
	}
}

// TestResume_ContinuationWithPriorMediaSurvivesEndlist covers P1 #1: a
// resumed continuation part whose segments were committed BEFORE the
// crash, where the resume's first poll sees ENDLIST with no new commits
// (the broadcast ended during downtime), must be finalized — not dropped
// by shouldFinalizeEmptyContinuation, which keyed "empty" off this run's
// SegmentsDone==0 and would lose the already-captured part (and let
// scratch cleanup delete its media).
//
// Discriminator: part 2 (its on-disk 103..105) is finalized. Without the
// resume-aware currentPartHasCommittedMedia check the loop breaks before
// runPart and part 2 never appears.
func TestResume_ContinuationWithPriorMediaSurvivesEndlist(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 6 // seqs 100..105
	opts.windowA = 3
	opts.dropAfterServed = 0
	opts.aEndlist = 6 // broadcast ends after all 6 served
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_priormedia",
		BroadcasterName:  "Harness PriorMedia",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_priormedia",
		DisplayName:      "Harness PriorMedia",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	videoID := job.VideoID

	// Let the first process commit the whole stream (100..105) to disk,
	// then crash before transitioning part 2 out of SEGMENTS.
	waitForJobResumeState(t, h.repo, jobID, func(s *ResumeState) bool {
		return s.AccountedFrontierMediaSeq >= int64(opts.baseSeqA+5)
	}, 30*time.Second)
	h.svc.Shutdown()

	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job pre-mutate: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal pre-mutate: %v", err)
	}

	// Part 2 holds 103..105, all committed before the crash.
	part01seg := filepath.Join(h.scratchDir, jobID, "part01", "segments")
	part02seg := filepath.Join(h.scratchDir, jobID, "part02", "segments")
	if err := os.MkdirAll(part02seg, 0o755); err != nil {
		t.Fatalf("mkdir part02 segments: %v", err)
	}
	for _, name := range []string{"103.ts", "104.ts", "105.ts"} {
		copySegment(t, part01seg, part02seg, name)
	}

	p1, err := h.repo.GetVideoPartByIndex(context.Background(), videoID, 1)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && p1 == nil) {
		p1, err = h.repo.CreateVideoPart(context.Background(), &repository.VideoPartInput{
			VideoID:       videoID,
			PartIndex:     1,
			Filename:      "harness_priormedia-part01.mp4",
			Quality:       state.SelectedQuality,
			FPS:           state.SelectedFPS,
			Codec:         state.SelectedCodec,
			SegmentFormat: "ts",
			StartMediaSeq: int64(opts.baseSeqA),
		})
	}
	if err != nil {
		t.Fatalf("get-or-create part 1: %v", err)
	}
	if err := h.repo.FinalizeVideoPart(context.Background(), &repository.VideoPartFinalize{
		ID:              p1.ID,
		DurationSeconds: 3.0,
		SizeBytes:       1000,
		EndMediaSeq:     int64(opts.baseSeqA + 2), // part 1 = 100..102
	}); err != nil {
		t.Fatalf("finalize part 1: %v", err)
	}

	// Part 2: anchored at 103, fully committed through 105, no pending
	// split, still in SEGMENTS so resume re-fetches and sees ENDLIST.
	state.SetStage(StageSegments)
	state.CurrentPartIndex = 2
	state.PartStarted = true
	state.PartStartMediaSequence = int64(opts.baseSeqA + 3) // 103
	state.AccountedFrontierMediaSeq = int64(opts.baseSeqA + 5)
	state.Gaps = nil
	state.CompletedAboveFrontier = nil
	state.PartBytes = 0
	state.PartDurationSeconds = 0
	state.PendingSplit = false
	state.PendingThresholdSplit = false
	state.PendingSplitBoundarySet = false
	state.EndListSeen = false
	if state.SegmentFormat == "" {
		state.SegmentFormat = "ts"
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal mutate: %v", err)
	}
	if err := h.repo.UpdateJobResumeState(context.Background(), jobID, data); err != nil {
		t.Fatalf("write mutated resume state: %v", err)
	}

	resumed := resumeOver(t, h, edge.URL())
	defer resumed.svc.Shutdown()
	if err := resumed.svc.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	video := waitForVideoStatus(t, resumed.repo, videoID, repository.VideoStatusDone, 60*time.Second)

	parts, err := resumed.repo.ListVideoParts(context.Background(), videoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}

	var p2 *repository.VideoPart
	for i := range parts {
		if parts[i].PartIndex == 2 {
			p2 = &parts[i]
		}
	}
	// The load-bearing assertion (P1 #1): part 2's prior media survived —
	// it was finalized at 105, not dropped as an empty continuation.
	if p2 == nil {
		t.Fatalf("part_index 2 missing — the resumed continuation's prior media was dropped; parts: %+v", parts)
	}
	if p2.EndMediaSeq == nil || *p2.EndMediaSeq != int64(opts.baseSeqA+5) {
		t.Fatalf("part 2 EndMediaSeq = %v, want %d", p2.EndMediaSeq, opts.baseSeqA+5)
	}
	if p2.StartMediaSeq != int64(opts.baseSeqA+3) {
		t.Errorf("part 2 StartMediaSeq = %d, want %d", p2.StartMediaSeq, opts.baseSeqA+3)
	}
	// Clean ENDLIST on a part that captured all its segments → not truncated.
	if video.Truncated {
		t.Error("video.Truncated=true, want false (the continuation captured its whole range and saw ENDLIST)")
	}
}

// TestMultipart_FMP4ByteThresholdSplitsContiguously is the fMP4 twin of
// the TS byte-threshold test: a single fMP4 variant is cut into multiple
// parts by MaxPartBytes. fMP4 has its own machinery the TS path doesn't —
// .m4s pruning of above-boundary in-flight segments, an init.mp4 refetch
// per continuation part, and fMP4 remux — so this exercises that code end
// to end. Variant A is dropped before Start so the recording uses the
// fMP4 variant (B, 360p) from the first poll.
func TestMultipart_FMP4ByteThresholdSplitsContiguously(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 1 // A is dropped; only needs to exist for fixture generation
	opts.fmp4Count = 4
	opts.windowB = 4
	opts.baseSeqB = 50
	opts.dropAfterServed = 0
	edge := newTwitchEdge(t, opts)

	// Drop variant A up front so the master advertises only the fMP4
	// variant (B); the recording resolves to it on the first poll.
	edge.mu.Lock()
	edge.aDropped = true
	edge.mu.Unlock()

	h := newHarnessService(t, edge.URL())
	defer h.svc.Shutdown()
	// A 1-byte ceiling is crossed by every (multi-KB) fMP4 segment, so each
	// committed segment becomes its own part.
	h.svc.cfg.App.Download.MaxPartBytes = 1

	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_fmp4split",
		BroadcasterName:  "Harness FMP4Split",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_fmp4split",
		DisplayName:      "Harness FMP4Split",
		Quality:          repository.QualityLow,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	video := waitForVideoStatus(t, h.repo, job.VideoID, repository.VideoStatusDone, 60*time.Second)

	parts, err := h.repo.ListVideoParts(context.Background(), job.VideoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != opts.fmp4Count {
		t.Fatalf("video_parts count = %d, want %d (1-byte ceiling cuts every fMP4 segment; parts: %+v)",
			len(parts), opts.fmp4Count, parts)
	}
	for i, p := range parts {
		wantSeq := int64(opts.baseSeqB + i)
		assertPartRange(t, p, wantSeq, wantSeq)
		if p.SegmentFormat != "fmp4" {
			t.Errorf("part %d segment_format = %q, want fmp4", p.PartIndex, p.SegmentFormat)
		}
		if p.Quality != "360" {
			t.Errorf("part %d quality = %q, want 360", p.PartIndex, p.Quality)
		}
		// Each fMP4 part must have a playable remux on disk (init.mp4 was
		// refetched + muxed with the .m4s); a missing/zero file means the
		// per-part init reuse or .m4s prune broke.
		path := filepath.Join(h.storageDir, "videos", p.Filename)
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Errorf("part %d storage file missing at %q: %v", p.PartIndex, path, statErr)
			continue
		}
		if info.Size() != p.SizeBytes || p.SizeBytes == 0 {
			t.Errorf("part %d storage size = %d, video_parts.size_bytes = %d (want equal, non-zero)",
				p.PartIndex, info.Size(), p.SizeBytes)
		}
	}

	// Contiguous tiling across the fMP4 cut: no gap, no duplicate, same
	// variant throughout — the .m4s prune + refetch preserved the
	// no-gap/no-duplicate invariant.
	assertContiguousCoverage(t, parts, int64(opts.baseSeqB), int64(opts.baseSeqB+opts.fmp4Count-1))

	// A size split is a clean cut, not a loss.
	if video.CompletionKind != repository.CompletionKindComplete {
		t.Errorf("completion_kind = %q, want %q (fMP4 size split is a clean cut)",
			video.CompletionKind, repository.CompletionKindComplete)
	}
}

// TestMultipart_ThresholdCountCapFailsCleanly proves the operator-facing
// runaway guard end to end: when configured threshold splitting would
// exceed max_part_count, the recording fails cleanly (a terminal FAILED
// video) rather than producing unbounded video_parts. MaxPartBytes=1 cuts
// every segment, so 3 segments want 3 parts but MaxPartCount=2 caps it —
// ShouldOpenNextPart aborts on the third.
func TestMultipart_ThresholdCountCapFailsCleanly(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 3
	opts.windowA = 3
	opts.dropAfterServed = 0
	opts.aEndlist = 3
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	defer h.svc.Shutdown()
	h.svc.cfg.App.Download.MaxPartBytes = 1 // cut every segment
	h.svc.cfg.App.Download.MaxPartCount = 2 // but cap at 2 parts

	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_capfail",
		BroadcasterName:  "Harness CapFail",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_capfail",
		DisplayName:      "Harness CapFail",
		Quality:          repository.QualityMedium,
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	// Runaway protection must terminate the job as FAILED, not hang or
	// silently complete with an unbounded part count.
	video := waitForVideoStatus(t, h.repo, job.VideoID, repository.VideoStatusFailed, 60*time.Second)
	if video.Status != repository.VideoStatusFailed {
		t.Fatalf("video status = %q, want %q (threshold split must abort at the cap)",
			video.Status, repository.VideoStatusFailed)
	}

	// The cap bounds the parts: never more than MaxPartCount finalized.
	parts, err := h.repo.ListVideoParts(context.Background(), job.VideoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) > 2 {
		t.Errorf("video_parts count = %d, want <= 2 (MaxPartCount cap); parts: %+v", len(parts), parts)
	}
}
