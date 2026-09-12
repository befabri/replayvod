package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
)

var errTerminalPersistence = errors.New("injected terminal persistence failure")

// Preserve the adapter's real transaction while failing a selected operation.
// The commit case fails after every callback write, before the real commit.
type terminalFaultRepo struct {
	repository.Repository
	fail string
}

func (r *terminalFaultRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		if err := fn(&terminalFaultRepo{Repository: tx, fail: r.fail}); err != nil {
			return err
		}
		if r.fail == "commit" {
			return errTerminalPersistence
		}
		return nil
	})
}

func (r *terminalFaultRepo) MarkVideoFailedAndEnqueueRecordingWebhook(ctx context.Context, id int64, message, kind string, truncated bool, delivery *repository.RecordingWebhookDeliveryInput) error {
	if r.fail == "video" {
		return errTerminalPersistence
	}
	return r.Repository.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, id, message, kind, truncated, delivery)
}

func (r *terminalFaultRepo) MarkJobFailed(ctx context.Context, id, message string) error {
	if r.fail == "job" {
		return errTerminalPersistence
	}
	return r.Repository.MarkJobFailed(ctx, id, message)
}

func (r *terminalFaultRepo) MarkJobDone(ctx context.Context, id string) error {
	if r.fail == "job" {
		return errTerminalPersistence
	}
	return r.Repository.MarkJobDone(ctx, id)
}

type queuedFailurePumpRepo struct {
	repository.Repository
	picks int
}

func (r *queuedFailurePumpRepo) GetNextQueuedArchiveJob(ctx context.Context) (*repository.Job, error) {
	r.picks++
	if r.picks > 1 {
		// Bound a regressed pump without relying on a timer or flooding logs.
		return nil, repository.ErrNotFound
	}
	return r.Repository.GetNextQueuedArchiveJob(ctx)
}

func TestArchivePumpDefersAfterStartFailureCannotBePersisted(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	ctx := t.Context()
	jobID, err := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "broken-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.UpdateJobResumeState(ctx, jobID, json.RawMessage("invalid")); err != nil {
		t.Fatal(err)
	}
	repo := &queuedFailurePumpRepo{Repository: &terminalFaultRepo{Repository: f.repo, fail: "job"}}
	f.svc.repo = repo
	f.svc.PumpArchiveQueue(ctx)
	if repo.picks != 1 {
		t.Fatalf("pump immediately retried an unchanged queued row: picks=%d", repo.picks)
	}
	job, err := f.repo.GetJob(ctx, jobID)
	if err != nil || job.Status != repository.JobStatusPending {
		t.Fatalf("uncommitted failure lost queued attempt: %+v %v", job, err)
	}
	f.svc.repo = f.repo
	f.svc.PumpArchiveQueue(ctx)
	job, err = f.repo.GetJob(ctx, jobID)
	if err != nil || job.Status != repository.JobStatusFailed {
		t.Fatalf("later pump did not persist the failure: %+v %v", job, err)
	}
}

func TestResumePendingLiveJobPreservesRecoveryWhenClaimFails(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	release := f.edge.hold()
	defer release()
	ctx := t.Context()
	const jobID = "pending-live"
	v, err := f.repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: "pending-live-rec", DisplayName: "bc-1", Status: repository.VideoStatusPending,
		Quality: repository.QualityHigh, BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := NewResumeState().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: v.ID, BroadcasterID: "bc-1", ResumeState: checkpoint}); err != nil {
		t.Fatal(err)
	}
	f.svc.repo = &archiveFaultRepo{Repository: f.repo, failVideoRunning: true}
	if err := f.svc.resumeRunning(ctx); !errors.Is(err, errArchiveClaim) {
		t.Fatalf("failed claim was treated as a recording failure: %v", err)
	}
	job, err := f.repo.GetJob(ctx, jobID)
	if err != nil || job.Status != repository.JobStatusPending || f.status(t, jobID) != repository.VideoStatusPending {
		t.Fatalf("failed claim lost pending recording: %+v %v", job, err)
	}
	f.svc.repo = f.repo
	if err := f.svc.resumeRunning(ctx); err != nil {
		t.Fatal(err)
	}
	if f.svc.Subscribe(jobID) == nil || f.status(t, jobID) != repository.VideoStatusRunning {
		t.Fatal("pending live recording did not resume after persistence recovered")
	}
}

func TestTerminalFailurePreservesRecoveryUntilCommit(t *testing.T) {
	for _, path := range []string{"download", "queued-start", "resume"} {
		for _, fail := range []string{"video", "job", "commit"} {
			t.Run(path+"/"+fail, func(t *testing.T) {
				f := newArchiveFixture(t, 1, 1)
				ctx := t.Context()
				jobID, err := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "terminal-rollback"))
				if err != nil {
					t.Fatal(err)
				}
				v := f.video(t, jobID)
				status := repository.VideoStatusRunning
				if path == "queued-start" {
					status = repository.VideoStatusPending
				} else {
					if err := f.repo.MarkJobRunning(ctx, jobID); err != nil {
						t.Fatal(err)
					}
					if err := f.repo.UpdateVideoStatus(ctx, v.ID, status); err != nil {
						t.Fatal(err)
					}
				}
				if path == "resume" {
					if err := f.repo.UpdateJobResumeState(ctx, jobID, json.RawMessage("not-json")); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := f.repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/test", "recording.failed"); err != nil {
					t.Fatal(err)
				}
				if err := f.repo.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
					t.Fatal(err)
				}
				scratch := filepath.Join(f.svc.cfg.Env.ScratchDir, jobID)
				if err := os.MkdirAll(scratch, 0o755); err != nil {
					t.Fatal(err)
				}
				checkpoint := filepath.Join(scratch, "checkpoint")
				if err := os.WriteFile(checkpoint, []byte("recoverable media"), 0o600); err != nil {
					t.Fatal(err)
				}
				bus := eventbus.New()
				f.svc.SetEventBus(bus)
				terminals := bus.RecordingTerminal.Subscribe(ctx)
				changes := bus.ArchiveQueue.Subscribe(ctx)
				job, err := f.repo.GetJob(ctx, jobID)
				if err != nil {
					t.Fatal(err)
				}
				d := &download{jobID: jobID, videoID: v.ID, broadcasterID: v.BroadcasterID, vod: true, attempt: 5, resume: NewResumeState()}
				run := func() {
					switch path {
					case "download":
						f.svc.failDownload(ctx, d, discardLog(), errors.New("permanent capture failure"))
					case "queued-start":
						f.svc.failQueuedArchive(ctx, job, errors.New("permanent start failure"))
					case "resume":
						_ = f.svc.resumeRunning(ctx)
					}
				}
				f.svc.repo = &terminalFaultRepo{Repository: f.repo, fail: fail}
				run()
				got := f.video(t, jobID)
				gotJob, err := f.repo.GetJob(ctx, jobID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != status || gotJob.Status != job.Status || d.cleanupScratch {
					t.Errorf("failed persistence lost recovery: video=%s job=%s cleanup=%v", got.Status, gotJob.Status, d.cleanupScratch)
				}
				if rows, err := f.repo.ListRecordingWebhookDeliveries(ctx, 10); err != nil || len(rows) != 0 {
					t.Errorf("uncommitted webhook: %+v, %v", rows, err)
				}
				if path == "queued-start" {
					if queue, err := f.repo.ListArchiveQueue(ctx); err != nil || len(queue) != 1 || queue[0].JobID != jobID {
						t.Errorf("attempt no longer queued: %+v, %v", queue, err)
					}
				} else if jobs, err := f.repo.ListRunningJobs(ctx); err != nil || len(jobs) != 1 {
					t.Errorf("attempt no longer recoverable: %+v, %v", jobs, err)
				}
				if data, err := os.ReadFile(checkpoint); err != nil || string(data) != "recoverable media" {
					t.Errorf("checkpoint lost: %q, %v", data, err)
				}
				select {
				case ev := <-terminals:
					t.Errorf("uncommitted terminal event: %+v", ev)
				default:
				}
				select {
				case ev := <-changes:
					t.Errorf("uncommitted queue event: %+v", ev)
				default:
				}
				if t.Failed() {
					return
				}
				// The same operation remains usable when persistence recovers.
				f.svc.repo = f.repo
				run()
				got = f.video(t, jobID)
				gotJob, err = f.repo.GetJob(ctx, jobID)
				if err != nil || got.Status != repository.VideoStatusFailed || gotJob.Status != repository.JobStatusFailed {
					t.Fatalf("terminal retry did not commit: video=%s job=%+v err=%v", got.Status, gotJob, err)
				}
				if rows, err := f.repo.ListRecordingWebhookDeliveries(ctx, 10); err != nil || len(rows) != 1 {
					t.Errorf("committed webhook: %+v, %v", rows, err)
				}
				select {
				case <-terminals:
				default:
					t.Error("committed failure did not notify subscribers")
				}
			})
		}
	}
}
