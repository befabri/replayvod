package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

type recoverySnapshotRepo struct {
	repository.Repository
	afterSnapshot func()
}

func (r recoverySnapshotRepo) ListRecoveryJobs(ctx context.Context, afterID string, limit int) ([]repository.Job, error) {
	jobs, err := r.Repository.ListRecoveryJobs(ctx, afterID, limit)
	if err == nil {
		r.afterSnapshot()
	}
	return jobs, err
}

func TestRecoveryPreservesRecordingStartedAfterDatabaseSnapshot(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	if err := f.svc.PrepareScratch(t.Context()); err != nil {
		t.Fatal(err)
	}
	playback := filepath.Join(f.svc.cfg.Env.ScratchDir, "playback-cache", "build-active", "parts.txt")
	if err := os.MkdirAll(filepath.Dir(playback), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(playback, []byte("active playback build"), 0o600); err != nil {
		t.Fatal(err)
	}
	var recording string
	f.svc.repo = recoverySnapshotRepo{Repository: f.repo, afterSnapshot: func() {
		jobID, err := enqueueAndPump(f.svc, t.Context(), vodParams("bc-1", "1001"))
		if err != nil {
			t.Fatal(err)
		}
		if f.svc.Subscribe(jobID) == nil {
			t.Fatal("recording not active during recovery")
		}
		recording = filepath.Join(f.svc.cfg.Env.ScratchDir, jobID, "part01", "segments", "committed.ts")
		if err := os.MkdirAll(filepath.Dir(recording), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(recording, []byte("committed recording data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	if err := f.svc.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{recording: "committed recording data", playback: "active playback build"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("recovery deleted active work %s: %q %v", path, got, err)
		}
	}
}
