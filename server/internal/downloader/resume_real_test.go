//go:build ffmpeg

// Resume-durability integration tests: clean crash/restart,
// MaxRestartGapSeconds threshold split, and PendingSplit /
// HadWindowRoll flag durability across resume. Build tag `ffmpeg`;
// shared primitives in harness_test.go.

package downloader

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
)

// TestResume_CrashMidStream_ResumesCleanly: AC-H264-4 in
// .docs/spec/download-pipeline.md. Start, shut down mid-stream,
// resume, recording completes as a single video_parts row.
func TestResume_CrashMidStream_ResumesCleanly(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 8
	opts.windowA = 8
	opts.dropAfterServed = 0
	opts.aEndlist = 8
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_resume",
		BroadcasterName:  "Harness Resume",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_resume",
		DisplayName:      "Harness Resume",
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

	// Resume only scans RUNNING/PENDING — verify shutdown didn't
	// fail the row.
	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job post-shutdown: %v", err)
	}
	if job.Status != repository.JobStatusRunning {
		t.Fatalf("post-shutdown job status = %q, want %q (shutdown should preserve RUNNING for resume)",
			job.Status, repository.JobStatusRunning)
	}
	v, err := h.repo.GetVideo(context.Background(), videoID)
	if err != nil {
		t.Fatalf("get video post-shutdown: %v", err)
	}
	if v.Status != repository.VideoStatusRunning {
		t.Fatalf("post-shutdown video status = %q, want %q", v.Status, repository.VideoStatusRunning)
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
	if len(parts) != 1 {
		t.Fatalf("video_parts count = %d, want 1 (no part split expected on clean resume; parts: %+v)",
			len(parts), parts)
	}

	// 0 or 1 restart_window_rolled entries depending on whether
	// the harness's sliding window actually rolled during shutdown
	// — accept either, fail only if more than one accumulated.
	job, err = resumed.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal final resume state: %v", err)
	}
	rollCount := 0
	for _, g := range state.Gaps {
		if g.Reason == GapReasonRestartWindowRolled {
			rollCount++
		}
	}
	if rollCount > 1 {
		t.Errorf("restart_window_rolled gap count = %d, want ≤ 1 (final gaps: %+v)",
			rollCount, state.Gaps)
	}

	wantKind := repository.CompletionKindComplete
	if rollCount > 0 {
		wantKind = repository.CompletionKindPartial
	}
	if video.CompletionKind != wantKind {
		t.Errorf("completion_kind = %q, want %q (rollCount=%d)",
			video.CompletionKind, wantKind, rollCount)
	}

	if video.DurationSeconds == nil || *video.DurationSeconds == 0 {
		t.Fatalf("video.duration_seconds unset or zero: %v", video.DurationSeconds)
	}
	if abs(*video.DurationSeconds-parts[0].DurationSeconds) > 0.001 {
		t.Errorf("video.duration_seconds = %f, want %f", *video.DurationSeconds, parts[0].DurationSeconds)
	}

	// Size match is the cheapest proxy for "ffmpeg produced a
	// complete playable file."
	storagePath := filepath.Join(resumed.storageDir, "videos", parts[0].Filename)
	info, err := os.Stat(storagePath)
	if err != nil {
		t.Fatalf("storage file missing at %q: %v", storagePath, err)
	}
	if info.Size() != parts[0].SizeBytes {
		t.Errorf("storage file size = %d, video_parts.size_bytes = %d", info.Size(), parts[0].SizeBytes)
	}
}

// TestResume_GapExceedsThreshold_ForcesSplit: post-restart playlist
// jumps far past the resume frontier; the resulting gap (>
// MaxRestartGapSeconds) forces a part boundary instead of a hole
// inside the file. Spec §"Resume on restart" point 5.
func TestResume_GapExceedsThreshold_ForcesSplit(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	// Fixture sized for the test's actual needs: shutdown at
	// frontier ~103 (4-6 segments served), post-jump cursor lands
	// past tsCount (clamped to aEndlist), part 02 gets the
	// trailing window plus ENDLIST. The stage rollback below
	// handles the SEGMENTS-vs-REMUX shutdown race regardless of
	// fixture length, so we don't need extra runway just for that.
	opts.tsCount = 20
	opts.windowA = 3
	opts.dropAfterServed = 0
	opts.aEndlist = 20         // ENDLIST after all segments served
	opts.fmp4Count = 0         // single variant — split is purely the gap threshold, not codec
	opts.baseSeqA = 100
	opts.postRestartSeqJump = 10 // post-jump start lands above frontier; lost > 2s threshold
	// Note: tsCount must exceed pre-shutdown cursor + jump so the
	// post-jump playlist serves seqs above the frontier — otherwise
	// the cursor clamps to tsCount and the served range overlaps
	// the frontier, no window roll fires, no split. Keep
	// tsCount > (frontier_at_shutdown - baseSeqA + windowA).

	edge := newTwitchEdge(t, opts)
	h := newHarnessServiceWithOpts(t, edge.URL(), harnessOpts{
		maxRestartGapSeconds: 2, // > 2s lost forces a split
	})
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_threshold",
		BroadcasterName:  "Harness Threshold",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_threshold",
		DisplayName:      "Harness Threshold",
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

	// Wait for ≥ 3 segments committed before shutdown so the
	// resumed run has prior content to finalize as part 01. The
	// hasPartContent guard blocks the split if there's no prior
	// content (a doom-loop guard); a too-short shutdown window
	// would produce a hard-fail instead of a split.
	waitForJobResumeState(t, h.repo, jobID, func(s *ResumeState) bool {
		return s.AccountedFrontierMediaSeq >= int64(opts.baseSeqA+3)
	}, 30*time.Second)

	h.svc.Shutdown()

	// Shutdown often races runPart past SEGMENTS into REMUX
	// (PrepareInput runs even after ctx cancel; ffmpeg fails
	// fast). Resume on stage>=PrepareInput skips
	// fetchWithAuthRefresh entirely, so OnWindowRoll never fires.
	// Roll back to SEGMENTS to deterministically test the
	// fetch-path branch — production reaches the same state when
	// a crash lands cleanly during Stage 4.
	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job pre-rollback: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal pre-rollback: %v", err)
	}
	if state.Stage.AtOrAfter(StagePrepareInput) {
		state.SetStage(StageSegments)
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatalf("marshal rollback: %v", err)
		}
		if err := h.repo.UpdateJobResumeState(context.Background(), jobID, data); err != nil {
			t.Fatalf("rollback resume state: %v", err)
		}
	}

	edge.NoteRestart()

	resumed := resumeOver(t, h, edge.URL(), withMaxRestartGapSeconds(2))
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
		t.Fatalf("video_parts count = %d, want 2 (threshold split expected; parts: %+v)",
			len(parts), parts)
	}

	// Same variant on both parts — the split was driven purely by
	// the gap threshold, not a codec change.
	if parts[0].Quality != parts[1].Quality {
		t.Errorf("part qualities differ (%q → %q); same variant expected",
			parts[0].Quality, parts[1].Quality)
	}
	if parts[0].SegmentFormat != parts[1].SegmentFormat {
		t.Errorf("part segment formats differ (%q → %q); same variant expected",
			parts[0].SegmentFormat, parts[1].SegmentFormat)
	}

	if parts[0].EndMediaSeq == nil {
		t.Fatalf("part 01 EndMediaSeq nil — finalization didn't land")
	}
	if *parts[0].EndMediaSeq < int64(opts.baseSeqA+3) {
		t.Errorf("part 01 EndMediaSeq = %d, want ≥ %d", *parts[0].EndMediaSeq, opts.baseSeqA+3)
	}
	// Equality is acceptable: the cancel race can commit a few
	// in-flight segments to part 01 before part 02 opens at the
	// same seq. Strict failure: a value below the prior frontier
	// would mean the new part's anchor regressed.
	if parts[1].StartMediaSeq < *parts[0].EndMediaSeq {
		t.Errorf("part 02 StartMediaSeq = %d, want ≥ part 01 EndMediaSeq (%d) — re-anchor regressed",
			parts[1].StartMediaSeq, *parts[0].EndMediaSeq)
	}

	if video.CompletionKind != repository.CompletionKindPartial {
		t.Errorf("completion_kind = %q, want %q",
			video.CompletionKind, repository.CompletionKindPartial)
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
		path := filepath.Join(resumed.storageDir, "videos", p.Filename)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("storage file missing for part %d at %q: %v", p.PartIndex, path, err)
			continue
		}
		if info.Size() != p.SizeBytes {
			t.Errorf("part %d storage size = %d, video_parts.size_bytes = %d", p.PartIndex, info.Size(), p.SizeBytes)
		}
	}
}

// TestResume_PendingSplitTrue_OpensPartNPlusOne: a process whose
// PendingSplit checkpoint landed before crashing mid-runPart must
// resume into a part N+1, not exit the loop after finalizing
// part N. Pins the integration-level "ShouldOpenNextPart consumes
// the persisted flag" contract.
func TestResume_PendingSplitTrue_OpensPartNPlusOne(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 12
	opts.windowA = 3
	opts.dropAfterServed = 0
	opts.aEndlist = 12
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_pending",
		BroadcasterName:  "Harness Pending",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_pending",
		DisplayName:      "Harness Pending",
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

	// Natural shutdown leaves Stage at REMUX — the
	// post-fetchWithAuthRefresh, mid-runPart point we want. Just
	// add the PendingSplit flag.
	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job pre-mutate: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal pre-mutate: %v", err)
	}
	state.PendingSplit = true
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
		t.Fatalf("video_parts count = %d, want 2 (parts: %+v)", len(parts), parts)
	}

	// BeginNewPart must consume PendingSplit; otherwise the loop
	// runs forever (capped by MaxPartsPerVideo, which would make
	// this assertion fire as len(parts) > 2 instead).
	job, err = resumed.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job post-resume: %v", err)
	}
	finalState, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal final: %v", err)
	}
	if finalState.PendingSplit {
		t.Errorf("PendingSplit still true after completion — BeginNewPart did not consume the flag")
	}

	if video.DurationSeconds == nil {
		t.Fatalf("video duration unset")
	}
	wantDur := parts[0].DurationSeconds + parts[1].DurationSeconds
	if abs(*video.DurationSeconds-wantDur) > 0.001 {
		t.Errorf("video.duration_seconds = %f, want sum of parts %f", *video.DurationSeconds, wantDur)
	}
}

// TestResume_HadWindowRollTrue_ClassifiesPartial: a process whose
// HadWindowRoll flag was set must produce completion_kind=partial
// on resume even if the resumed run itself observes no roll. Pins
// the integration-level "run() seeds from the persisted field"
// contract.
func TestResume_HadWindowRollTrue_ClassifiesPartial(t *testing.T) {
	requireFFmpegHarness(t)

	opts := defaultEdgeOpts()
	opts.tsCount = 12
	opts.windowA = 3
	opts.dropAfterServed = 0
	opts.aEndlist = 12
	opts.fmp4Count = 0
	opts.baseSeqA = 100
	edge := newTwitchEdge(t, opts)

	h := newHarnessService(t, edge.URL())
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_hadwindow",
		BroadcasterName:  "Harness HadWindow",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_hadwindow",
		DisplayName:      "Harness HadWindow",
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

	// Mutate resume_state: simulate "the prior process's part 1
	// observed a CDN window roll before this snapshot." The flag
	// is the only durable record of that event — the prior gap
	// entries have rolled past the per-part frontier scope by
	// the time we resume.
	job, err = h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job pre-mutate: %v", err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal pre-mutate: %v", err)
	}
	state.HadWindowRoll = true
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

	if video.CompletionKind != repository.CompletionKindPartial {
		t.Errorf("completion_kind = %q, want %q",
			video.CompletionKind, repository.CompletionKindPartial)
	}

	// Guards against a false positive: the resumed run must not
	// itself record a new window roll. If it did, partial would
	// be set for the wrong reason.
	job, err = resumed.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job post-resume: %v", err)
	}
	finalState, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatalf("unmarshal final: %v", err)
	}
	for _, g := range finalState.Gaps {
		if g.Reason == GapReasonRestartWindowRolled {
			t.Fatalf("resumed run recorded a window-roll gap (%+v); test setup wrong", g)
		}
	}
	if !finalState.HadWindowRoll {
		t.Errorf("HadWindowRoll = false after completion; expected true")
	}
}
