package downloader

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// seedRunningJobWithCorruptResume creates a RUNNING job whose resume_state is
// invalid JSON, so restartJob fails on the parse step and resumeInflightJobs
// runs its terminal-failure path. When withFinalizedPart is true the recording
// already persisted a part (size_bytes > 0), the signal HasFinalizedVideoParts
// keys on. Returns the video ID.
func seedRunningJobWithCorruptResume(t *testing.T, ctx context.Context, repo repository.Repository, jobID, broadcasterID string, withFinalizedPart bool) int64 {
	t.Helper()
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: jobID + "-rec", DisplayName: broadcasterID, Status: repository.VideoStatusPending,
		Quality: "HIGH", BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create video %s: %v", jobID, err)
	}
	if withFinalizedPart {
		part, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: 1, Filename: jobID + "-rec-part01.mp4",
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		})
		if err != nil {
			t.Fatalf("create part %s: %v", jobID, err)
		}
		if err := repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{
			ID: part.ID, DurationSeconds: 60, SizeBytes: 2048, EndMediaSeq: 10,
		}); err != nil {
			t.Fatalf("finalize part %s: %v", jobID, err)
		}
	}
	if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: v.ID, BroadcasterID: broadcasterID}); err != nil {
		t.Fatalf("create job %s: %v", jobID, err)
	}
	if err := repo.UpdateJobResumeState(ctx, jobID, json.RawMessage("not-json")); err != nil {
		t.Fatalf("set corrupt resume state %s: %v", jobID, err)
	}
	if err := repo.MarkJobRunning(ctx, jobID); err != nil {
		t.Fatalf("mark job running %s: %v", jobID, err)
	}
	return v.ID
}

// TestResume_FailedRestartClassifiesPartialWhenPartsFinalized pins the
// resume-failure completion_kind classification: a job that already finalized
// parts before failing to restart owns reclaimable objects, so it is stamped
// FAILED/partial to stay inside the retention sweep (which only selects DONE plus
// FAILED partial/cancelled). A job with no finalized parts has nothing to
// reclaim and stays FAILED/complete. Without the classification both would land
// "complete" and the salvaged one's uploaded parts would escape retention.
func TestResume_FailedRestartClassifiesPartialWhenPartsFinalized(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, t.TempDir())

	if _, err := s.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b-1", BroadcasterName: "b-1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	salvaged := seedRunningJobWithCorruptResume(t, ctx, s.repo, "job-salvaged", "b-1", true)
	lost := seedRunningJobWithCorruptResume(t, ctx, s.repo, "job-lost", "b-1", false)

	if err := s.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	gotSalvaged, err := s.repo.GetVideo(ctx, salvaged)
	if err != nil {
		t.Fatalf("GetVideo salvaged: %v", err)
	}
	if gotSalvaged.Status != repository.VideoStatusFailed {
		t.Fatalf("salvaged status = %q, want FAILED", gotSalvaged.Status)
	}
	if gotSalvaged.CompletionKind != repository.CompletionKindPartial {
		t.Fatalf("salvaged completion_kind = %q, want %q (finalized parts must keep it in retention)",
			gotSalvaged.CompletionKind, repository.CompletionKindPartial)
	}

	gotLost, err := s.repo.GetVideo(ctx, lost)
	if err != nil {
		t.Fatalf("GetVideo lost: %v", err)
	}
	if gotLost.Status != repository.VideoStatusFailed {
		t.Fatalf("lost status = %q, want FAILED", gotLost.Status)
	}
	if gotLost.CompletionKind != repository.CompletionKindComplete {
		t.Fatalf("lost completion_kind = %q, want %q (no finalized parts to reclaim)",
			gotLost.CompletionKind, repository.CompletionKindComplete)
	}
}
