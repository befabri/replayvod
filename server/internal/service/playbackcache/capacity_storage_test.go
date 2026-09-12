package playbackcache

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type fullStorageProbe struct {
	*storage.LocalStorage
	cause error
}

func (s fullStorageProbe) ProbeWrite(context.Context) error {
	return &os.PathError{Op: "create", Path: s.Root, Err: s.cause}
}

func TestFullDiskPermitsCachePruningAndPausesBuilds(t *testing.T) {
	for _, cause := range []error{syscall.ENOSPC, syscall.EDQUOT} {
		for _, reconcile := range []bool{false, true} {
			name := "prune"
			if reconcile {
				name = "reconcile"
			}
			t.Run(cause.Error()+"/"+name, func(t *testing.T) {
				svc, repo, local, runner := cacheStorageFixture(t, nil)
				ctx := t.Context()
				attached, err := storage.Attach(ctx, local, "")
				if err != nil {
					t.Fatal(err)
				}
				probe := fullStorageProbe{local, cause}
				svc.gate = gateFunc(func() error { return storage.Ready(ctx, probe, attached.ID) })
				if err := svc.BuildNow(ctx, 42); !errors.Is(err, storage.ErrFull) {
					t.Fatalf("build on full storage: %v", err)
				}
				if runner.calls != 0 || repo.asset != nil {
					t.Fatal("full disk admitted a build")
				}
				name := "vod-42-playback.mp4"
				size := int64(14)
				if err := local.Save(ctx, storagekeys.Video(name), strings.NewReader("existing cache")); err != nil {
					t.Fatal(err)
				}
				repo.ready = []repository.VideoPlaybackAsset{{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size}}
				svc.fsStat = func(string) (int64, int64, error) { return 1000, 0, nil }
				prune := svc.Prune
				if reconcile {
					prune = svc.Reconcile
				}
				if err := prune(ctx); err != nil {
					t.Fatalf("full disk blocked pruning: %v", err)
				}
				if len(repo.ready) != 0 {
					t.Fatal("prune retained cache row")
				}
				if exists, err := local.Exists(ctx, storagekeys.Video(name)); err != nil || exists {
					t.Fatalf("prune failed to free cache bytes: exists=%v err=%v", exists, err)
				}
			})
		}
	}
}

func TestBuildThatFillsStorageReclaimsItsUnpublishedArtifact(t *testing.T) {
	var gateErr error
	svc, repo, local, runner := cacheStorageFixture(t, gateFunc(func() error { return gateErr }))
	runner.beforeWrite = func() { gateErr = storage.ErrFull }
	if err := svc.BuildNow(t.Context(), 42); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("full disk build: %v", err)
	}
	if runner.calls != 1 || repo.asset == nil || repo.asset.Status == repository.PlaybackAssetStatusReady {
		t.Fatalf("full disk finalized a playback build: calls=%d asset=%+v", runner.calls, repo.asset)
	}
	if exists, err := local.Exists(t.Context(), storagekeys.Video("vod-42-playback.mp4")); err != nil || exists {
		t.Fatalf("unpublished artifact prevents reclamation: exists=%v err=%v", exists, err)
	}
}
