package downloader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

type interruptedPublication struct {
	storage.Storage
	entered chan struct{}
}

func (s interruptedPublication) Save(ctx context.Context, _ string, _ io.Reader) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestStopDuringPreparedUpload(t *testing.T) {
	for _, scenario := range []string{"live", "archive", "shutdown"} {
		t.Run(scenario, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			d := seedWebhookAttempt(t, s, "upload")
			d.vod = scenario == "archive"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d.runCtx, d.cancel = ctx, cancel
			s.active[d.jobID] = d
			raw, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			blocked := interruptedPublication{Storage: raw, entered: make(chan struct{})}
			s.storage = mediatest.NewAt(t, s.repo, blocked, nil, nil, s.cfg.Env.ScratchDir)
			output := filepath.Join(t.TempDir(), "prepared.mp4")
			if err := os.WriteFile(output, []byte("prepared bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			digest, err := preparedDigest(ctx, output)
			if err != nil {
				t.Fatal(err)
			}
			d.resume.PreparedPart = &PreparedPart{Filename: "prepared.mp4", Path: output, Digest: digest}
			d.resume.Stage = StageStore
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, err := s.publishPreparedPart(ctx, d, d.resume.PreparedPart, s.log)
				s.failDownload(t.Context(), d, s.log, err)
			}()
			select {
			case <-blocked.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("publication did not reach backend")
			}
			if scenario == "shutdown" {
				s.shuttingDown.Store(true)
				cancel()
			} else if err := s.Cancel(d.jobID); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("publication did not settle")
			}
			job, err := s.repo.GetJob(t.Context(), d.jobID)
			if err != nil {
				t.Fatal(err)
			}
			video, err := s.repo.GetVideo(t.Context(), d.videoID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "shutdown" {
				if job.StopRequested || job.Status != repository.JobStatusRunning || d.cleanupScratch {
					t.Fatalf("shutdown lost recoverable upload: %+v", job)
				}
			} else if !job.StopRequested || job.Status != repository.JobStatusFailed || video.CompletionKind != repository.CompletionKindCancelled || video.NextRetryAt != nil || !d.cleanupScratch {
				t.Fatalf("Stop lost cancellation during upload: %+v, %+v", job, video)
			}
		})
	}
}

func TestStopPreparedPublicationSettlesCancellation(t *testing.T) {
	for _, vod := range []bool{false, true} {
		t.Run(map[bool]string{false: "live without intent", true: "archive"}[vod], func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			d := seedWebhookAttempt(t, s, "stop-prepared")
			d.vod = vod
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d.runCtx, d.cancel = ctx, cancel
			s.active[d.jobID] = d
			if err := s.Cancel(d.jobID); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "prepared.mp4")
			if err := os.WriteFile(output, []byte("prepared output"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := s.publishPreparedPart(ctx, d, &PreparedPart{Path: output}, s.log)
			if err == nil {
				t.Fatal("cancelled publication succeeded")
			}
			s.failDownload(t.Context(), d, s.log, err)
			job, err := s.repo.GetJob(t.Context(), d.jobID)
			if err != nil || job.Status != repository.JobStatusFailed {
				t.Fatalf("acknowledged Stop left job recoverable: %+v, %v", job, err)
			}
			video, err := s.repo.GetVideo(t.Context(), d.videoID)
			if err != nil || video.CompletionKind != repository.CompletionKindCancelled || video.NextRetryAt != nil || !d.cleanupScratch {
				t.Fatalf("Stop lost terminal cancellation: %+v, cleanup=%v, %v", video, d.cleanupScratch, err)
			}
			jobs, err := s.repo.ListRecoveryJobs(t.Context(), "", 100)
			if err != nil || len(jobs) != 0 {
				t.Fatalf("cancelled attempt rediscovered: %+v, %v", jobs, err)
			}
		})
	}
}

type partCommitFault struct {
	repository.Repository
	writes *atomic.Int64
	lose   *atomic.Bool
}

func (r partCommitFault) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	before := r.writes.Load()
	err := r.Repository.WithTx(ctx, func(tx repository.Repository) error { return fn(partCommitFault{tx, r.writes, r.lose}) })
	if err == nil && r.writes.Load() > before && r.lose.CompareAndSwap(true, false) {
		return repository.ErrCommitUncertain
	}
	return err
}
func (r partCommitFault) FinalizeVideoPart(ctx context.Context, f *repository.VideoPartFinalize) error {
	r.writes.Add(1)
	return r.Repository.FinalizeVideoPart(ctx, f)
}
func TestPreparedPartRecoveryReusesExactOutputAfterLostFinalizationCommit(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	svc.cfg.App.Download.MaxPartBytes = 10
	d := seedWebhookAttempt(t, svc, "prepared")
	ctx := t.Context()
	d.runCtx = ctx
	part, err := svc.repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: d.videoID, PartIndex: 1, Filename: "captured.mp4", Quality: repository.QualityHigh, Codec: repository.CodecH264, SegmentFormat: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "captured.mp4")
	if err := os.WriteFile(output, []byte("exact remuxed output"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := preparedDigest(ctx, output)
	if err != nil {
		t.Fatal(err)
	}
	d.resume.PreparedPart = &PreparedPart{Filename: "captured.mp4", Path: output, Digest: digest, Facts: repository.VideoPartFinalize{ID: part.ID, SizeBytes: 20, DurationSeconds: 12}}
	d.resume.PartBytes = 10
	d.resume.Stage = StageStore
	checkpoint, err := d.resume.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	d.resume, err = UnmarshalResumeState(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	writes := &atomic.Int64{}
	lose := &atomic.Bool{}
	lose.Store(true)
	svc.repo = partCommitFault{svc.repo, writes, lose}
	// No valid segments or ffmpeg binary: recovery must only publish and settle.
	svc.remuxer.FFmpegPath = "/does-not-exist"
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	result, err := svc.runPart(ctx, ctx, d, Params{}, "unused", "/missing/segments", &hls.JobResult{}, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 2 || result.localPath != output || result.durationSeconds != 12 {
		t.Fatalf("lost commit was not reconciled from exact facts: %+v writes=%d", result, writes.Load())
	}
	if output := logs.String(); !strings.Contains(output, `"msg":"part complete"`) || !strings.Contains(output, `"level":"WARN"`) || !strings.Contains(output, `"source_bytes":10`) || !strings.Contains(output, `"size_bytes":20`) {
		t.Fatalf("recovered publication lost its source-size diagnostic: %s", output)
	}
	saved, err := svc.storage.Open(ctx, storagekeys.Video("captured.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	_ = saved.Close()
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("part settlement removed attempt scratch: %v", err)
	}
	if err := os.WriteFile(output, []byte("different media"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.runPart(ctx, ctx, d, Params{}, "unused", "/missing", nil, nil, svc.log); err == nil || d.persistenceErr == nil {
		t.Fatal("changed prepared bytes accepted")
	}
	if errors.Is(d.persistenceErr, repository.ErrNotFound) {
		t.Fatal("unexpected missing-row error")
	}
}
