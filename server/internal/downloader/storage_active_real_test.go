//go:build ffmpeg

package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type observedStorageGate struct {
	*storagehealth.Monitor
	refused chan struct{}
	once    sync.Once
}

func newObservedStorageGate(mon *storagehealth.Monitor) *observedStorageGate {
	return &observedStorageGate{Monitor: mon, refused: make(chan struct{})}
}

func (g *observedStorageGate) Verify(ctx context.Context) error {
	err := g.Monitor.Verify(ctx)
	if err != nil {
		g.once.Do(func() { close(g.refused) })
	}
	return err
}

type storageLossAfterPartRepo struct {
	repository.Repository
	after func() error
}

func (r storageLossAfterPartRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(storageLossAfterPartRepo{Repository: tx, after: r.after})
	})
}

func (r storageLossAfterPartRepo) FinalizeVideoPart(ctx context.Context, input *repository.VideoPartFinalize) error {
	if err := r.Repository.FinalizeVideoPart(ctx, input); err != nil {
		return err
	}
	return r.after()
}

func TestRealArchive_MountLostDuringUploadCannotFinalizeOrCleanScratch(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 2, windowA: 1, baseSeqA: 100, baseSeqB: 50})
	h := newHarnessService(t, edge.URL())
	t.Cleanup(h.svc.Shutdown)
	ctx := t.Context()
	seedArchiveChannel(t, h.repo, "archivist")
	store := &detachOnSaveStorage{LocalStorage: h.storage.(*storage.LocalStorage)}
	h.svc.storage = mediatest.NewAt(t, h.svc.repo, store, nil, nil, h.scratchDir)
	mon := storagehealth.New(h.repo, store, nil, h.svc.log, "local", h.storageDir)
	if _, err := mon.Attach(ctx); err != nil {
		t.Fatal(err)
	}
	gate := newObservedStorageGate(mon)
	setDownloaderGate(t, h.svc, gate)
	detached := filepath.Join(t.TempDir(), "detached-storage")
	store.detach = func() error {
		if err := os.Rename(h.storageDir, detached); err != nil {
			return err
		}
		return os.Mkdir(h.storageDir, 0o755)
	}
	jobID, err := enqueueAndPump(h.svc, ctx, Params{BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST", Title: "mount lost during upload", Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo, VODID: "7171"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.refused:
	case <-time.After(10 * time.Second):
		t.Fatal("upload never rejected the replacement volume")
	}
	v, err := h.repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := h.repo.GetJob(ctx, jobID)
	if err != nil || v.Status != repository.VideoStatusRunning || job.Status != repository.JobStatusRunning || job.Attempt != 1 {
		t.Fatalf("untrusted upload finalized recording: video=%s job=%+v err=%v", v.Status, job, err)
	}
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 || parts[0].SizeBytes != 0 {
		t.Fatalf("untrusted upload finalized part: %+v err=%v", parts, err)
	}
	scratch := filepath.Join(h.scratchDir, jobID)
	if files, err := os.ReadDir(scratch); err != nil || len(files) == 0 {
		t.Fatalf("upload discarded recovery data: %v %v", files, err)
	}
	if err := os.RemoveAll(h.storageDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(detached, h.storageDir); err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusDone, 10*time.Second)
	h.svc.Shutdown()
	parts, err = h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 || parts[0].SizeBytes <= 0 {
		t.Fatalf("trusted retry missing: %+v %v", parts, err)
	}
	if info, err := h.storage.Stat(ctx, storagekeys.Video(parts[0].Filename)); err != nil || info.Size != parts[0].SizeBytes {
		t.Fatalf("trusted recording is incomplete: %+v %v", info, err)
	}
	if _, err := os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful recovery did not clean scratch: %v", err)
	}
}

func TestRealArchive_StorageOutageBeforeCompletion(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 2, windowA: 1, baseSeqA: 100, baseSeqB: 50})
	h := newHarnessService(t, edge.URL())
	t.Cleanup(h.svc.Shutdown)
	ctx := t.Context()
	seedArchiveChannel(t, h.repo, "archivist")
	mon := storagehealth.New(h.repo, h.storage, nil, h.svc.log, "local", h.storageDir)
	if _, err := mon.Attach(ctx); err != nil {
		t.Fatal(err)
	}
	identity, err := storage.ReadMarker(ctx, h.storage)
	if err != nil {
		t.Fatal(err)
	}
	gate := newObservedStorageGate(mon)
	refused := gate.refused
	setDownloaderGate(t, h.svc, gate)
	h.svc.repo = storageLossAfterPartRepo{Repository: h.repo, after: func() error {
		if err := storage.WriteMarker(ctx, h.storage, strings.Repeat("f", 64)); err != nil {
			return err
		}
		return nil
	}}
	jobID, err := enqueueAndPump(h.svc, ctx, Params{BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST", Title: "storage lost after finalized part", Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo, VODID: "9191"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-refused:
	case <-time.After(5 * time.Second):
		t.Fatal("completion never checked the storage gate")
	}
	v, err := h.repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 || parts[0].SizeBytes <= 0 {
		t.Fatalf("fixture never finalized media: %+v err=%v", parts, err)
	}
	job, err := h.repo.GetJob(ctx, jobID)
	if err != nil || v.Status != repository.VideoStatusRunning || job.Status != repository.JobStatusRunning {
		t.Fatalf("completion escaped the gate: video=%s job=%+v err=%v", v.Status, job, err)
	}
	if err := storage.WriteMarker(ctx, h.storage, identity); err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusDone, 5*time.Second)
}

func TestRealArchive_StorageOutagePreservesActiveAttempt(t *testing.T) {
	requireFFmpegHarness(t)
	for _, restart := range []bool{false, true} {
		name := "reattach"
		if restart {
			name = "restart"
		}
		t.Run(name, func(t *testing.T) {
			edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 2, windowA: 1, baseSeqA: 100, baseSeqB: 50})
			h := newHarnessService(t, edge.URL())
			t.Cleanup(h.svc.Shutdown)
			ctx := t.Context()
			seedArchiveChannel(t, h.repo, "archivist")
			mon := storagehealth.New(h.repo, h.storage, nil, h.svc.log, "local", h.storageDir)
			if _, err := mon.Attach(ctx); err != nil {
				t.Fatal(err)
			}
			identity, err := storage.ReadMarker(ctx, h.storage)
			if err != nil {
				t.Fatal(err)
			}
			gate := newObservedStorageGate(mon)
			refused := gate.refused
			setDownloaderGate(t, h.svc, gate)
			bus := eventbus.New()
			h.svc.SetEventBus(bus)
			terminals := bus.RecordingTerminal.Subscribe(ctx)
			release := edge.BlockGQL()
			defer release()
			jobID, err := enqueueAndPump(h.svc, ctx, Params{BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST", Title: "storage outage", Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo, VODID: "8181"})
			if err != nil {
				t.Fatal(err)
			}
			progress := h.svc.Subscribe(jobID)
			if progress == nil {
				t.Fatal("recording never started")
			}
			if err := storage.WriteMarker(ctx, h.storage, strings.Repeat("f", 64)); err != nil {
				t.Fatal(err)
			}
			if err := mon.Ready(); err != nil {
				t.Fatalf("fixture needs stale cached success: %v", err)
			}
			release()
			timer := time.NewTimer(20 * time.Second)
			defer timer.Stop()
		waitForRefusal:
			for {
				select {
				case <-refused:
					break waitForRefusal
				case _, ok := <-progress:
					if !ok {
						t.Fatal("recording exited without checking the foreign storage before upload")
					}
				case <-timer.C:
					t.Fatal("recording did not reach the storage boundary")
				}
			}
			v, err := h.repo.GetVideoByJobID(ctx, jobID)
			if err != nil {
				t.Fatal(err)
			}
			job, err := h.repo.GetJob(ctx, jobID)
			if err != nil {
				t.Fatal(err)
			}
			if v.Status != repository.VideoStatusRunning || job.Status != repository.JobStatusRunning || job.Attempt != 1 {
				t.Fatalf("storage outage consumed attempt: video=%s job=%+v", v.Status, job)
			}
			state, err := UnmarshalResumeState(job.ResumeState)
			if err != nil || state.Stage != StageStore {
				t.Fatalf("missing durable upload checkpoint: %+v err=%v", state, err)
			}
			parts, err := h.repo.ListVideoParts(ctx, v.ID)
			if err != nil || len(parts) != 1 || parts[0].SizeBytes != 0 {
				t.Fatalf("part finalized on refused storage: %+v err=%v", parts, err)
			}
			if exists, err := h.storage.Exists(ctx, storagekeys.Video(parts[0].Filename)); err != nil || exists {
				t.Fatalf("wrote media onto foreign storage: exists=%v err=%v", exists, err)
			}
			scratch := filepath.Join(h.scratchDir, jobID)
			if files, err := os.ReadDir(scratch); err != nil || len(files) == 0 {
				t.Fatalf("recovery scratch lost: %v err=%v", files, err)
			}
			select {
			case ev := <-terminals:
				t.Fatalf("premature terminal event: %+v", ev)
			default:
			}
			if restart {
				h.svc.Shutdown()
				job, err = h.repo.GetJob(ctx, jobID)
				if err != nil || job.Status != repository.JobStatusRunning {
					t.Fatalf("shutdown failed paused attempt: %+v err=%v", job, err)
				}
			}
			if err := storage.WriteMarker(ctx, h.storage, identity); err != nil {
				t.Fatal(err)
			}
			if mon.Check(ctx).State != storagehealth.StateAttached {
				t.Fatal("storage did not reattach")
			}
			if restart {
				h = resumeOver(t, h, edge.URL())
				t.Cleanup(h.svc.Shutdown)
				setDownloaderGate(t, h.svc, mon)
				if err := h.svc.Resume(ctx); err != nil {
					t.Fatal(err)
				}
			}
			// The same-process case must recover without needing another attach
			// notification or manual Resume call.
			waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusDone, 20*time.Second)
			h.svc.Shutdown()
			job, err = h.repo.GetJob(ctx, jobID)
			if err != nil || job.Status != repository.JobStatusDone || job.Attempt != 1 {
				t.Fatalf("recovery did not finish the same attempt: %+v err=%v", job, err)
			}
			got, err := h.repo.ListVideoParts(ctx, v.ID)
			if err != nil || len(got) != 1 || got[0].ID != parts[0].ID || got[0].SizeBytes <= 0 {
				t.Fatalf("recovery lost or duplicated media: %+v err=%v", got, err)
			}
			if _, err := os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("successful recovery left scratch: %v", err)
			}
		})
	}
}
