package playbackcache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

func TestCacheRefusesUnavailableStorage(t *testing.T) {
	for _, verdict := range []struct {
		name string
		err  error
	}{
		{"unattached", storage.ErrUnattached}, {"unreachable", storage.ErrUnreachable}, {"read-only", storage.ErrReadOnly},
	} {
		t.Run(verdict.name, func(t *testing.T) {
			svc, repo, store, runner := cacheFixture(t, gateFunc(func() error { return verdict.err }))
			ctx := t.Context()
			if err := svc.BuildNow(ctx, 42); !errors.Is(err, verdict.err) {
				t.Errorf("build error=%v, want %v", err, verdict.err)
			}
			if runner.calls != 0 || repo.asset != nil {
				t.Errorf("unavailable storage started build: calls=%d asset=%+v", runner.calls, repo.asset)
			}
			name := "vod-42-playback.mp4"
			if err := store.Save(ctx, storagekeys.Video(name), strings.NewReader("existing cache")); err != nil {
				t.Fatal(err)
			}
			size := int64(14)
			repo.ready = []repository.VideoPlaybackAsset{{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size}}
			svc.fsStat = func(string) (int64, int64, error) { return 1000, 0, nil }
			for _, prune := range []func(context.Context) error{svc.Prune, svc.Reconcile} {
				_ = prune(ctx)
				if len(repo.ready) != 1 {
					t.Error("prune removed cache reference during storage outage")
				}
				if exists, err := store.Exists(ctx, storagekeys.Video(name)); err != nil || !exists {
					t.Errorf("prune deleted existing cache: exists=%v err=%v", exists, err)
				}
			}
			owned, _ := svc.store.Lock(ctx, 42)
			_ = svc.deleteArtifact(ctx, owned, name)
			owned.Close()
			if exists, err := store.Exists(ctx, storagekeys.Video(name)); err != nil || !exists {
				t.Errorf("cleanup deleted existing cache: exists=%v err=%v", exists, err)
			}
		})
	}
}

func TestCacheReconcilePreservesForeignStorage(t *testing.T) {
	svc, repo, store, _ := cacheFixture(t, nil)
	ctx := t.Context()
	expected := strings.Repeat("a", 64)
	if err := storage.WriteMarker(ctx, store, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	mediatest.SetGate(svc.store, gateFunc(func() error { return storage.Ready(ctx, store, expected) }))
	if err := svc.store.Verify(ctx); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("fixture not foreign: %v", err)
	}
	name := "vod-42-playback.mp4"
	size := int64(14)
	if err := store.Save(ctx, storagekeys.Video(name), strings.NewReader("foreign object")); err != nil {
		t.Fatal(err)
	}
	repo.ready = []repository.VideoPlaybackAsset{{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size}}
	svc.fsStat = func(string) (int64, int64, error) { return 1000, 0, nil }
	_ = svc.Reconcile(ctx)
	if len(repo.ready) != 1 {
		t.Error("reconcile discarded cache reference on foreign storage")
	}
	if exists, err := store.Exists(ctx, storagekeys.Video(name)); err != nil || !exists {
		t.Errorf("reconcile deleted foreign object: exists=%v err=%v", exists, err)
	}
	// Once explicitly attached to the expected identity, eviction works again.
	if err := storage.WriteMarker(ctx, store, expected); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(repo.ready) != 0 {
		t.Error("attached storage did not resume pruning")
	}
	if exists, err := store.Exists(ctx, storagekeys.Video(name)); err != nil || exists {
		t.Errorf("attached prune failed: exists=%v err=%v", exists, err)
	}
}

type storageLossRunner struct {
	afterStart func()
}

func (r storageLossRunner) Concat(context.Context, string, string) error {
	r.afterStart()
	return errors.New("storage disconnected during concat")
}

func TestCacheBuildDoesNotCleanUpAfterStorageChanges(t *testing.T) {
	var gateErr error
	svc, repo, store, _ := cacheFixture(t, gateFunc(func() error { return gateErr }))
	name := "vod-42-playback.mp4"
	svc.SetRunner(storageLossRunner{afterStart: func() {
		gateErr = storage.ErrUnattached
		if err := store.Save(t.Context(), storagekeys.Video(name), strings.NewReader("other install's cache")); err != nil {
			t.Fatal(err)
		}
	}})
	if err := svc.BuildNow(t.Context(), 42); err == nil {
		t.Fatal("build unexpectedly succeeded")
	}
	path, err := store.LocalPath(storagekeys.Video(name))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "other install's cache" {
		t.Errorf("cleanup changed foreign file: %q err=%v", data, err)
	}
	assertPlaybackFiles(t, svc, store, name)
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Errorf("storage outage recorded a terminal cache verdict: %+v", repo.asset)
	}
}

func TestCacheBuildRechecksStorageBeforeUpload(t *testing.T) {
	var gateErr error
	svc, repo, store, runner := cacheFixture(t, gateFunc(func() error { return gateErr }))
	// Wrapping the backend forces object-storage capacity budgeting.
	svc.store = cacheMedia(t, repo, struct{ storage.Storage }{store}, gateFunc(func() error { return gateErr }), nil)
	svc.capacityOverride = func(int64) (int64, bool) { return 1000, true }
	runner.beforeWrite = func() { gateErr = storage.ErrReadOnly }
	if err := svc.BuildNow(t.Context(), 42); !errors.Is(err, storage.ErrReadOnly) {
		t.Fatalf("storage change during concat: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("concat never ran: %d calls", runner.calls)
	}
	assertPlaybackFiles(t, svc, store)
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Errorf("interrupted upload left a terminal verdict: %+v", repo.asset)
	}
}

func TestCacheBuildPreservesReplacementVolumeDuringConcat(t *testing.T) {
	svc, repo, store, runner := cacheFixture(t, nil)
	ctx := t.Context()
	expected := strings.Repeat("a", 64)
	if err := storage.WriteMarker(ctx, store, expected); err != nil {
		t.Fatal(err)
	}
	mediatest.SetGate(svc.store, gateFunc(func() error { return storage.Ready(ctx, store, expected) }))
	name := "vod-42-playback.mp4"
	path, err := store.LocalPath(storagekeys.Video(name))
	if err != nil {
		t.Fatal(err)
	}
	detached := filepath.Join(t.TempDir(), "detached")
	runner.beforeWrite = func() {
		if err := os.Rename(store.Root, detached); err != nil {
			t.Fatal(err)
		}
		if err := storage.WriteMarker(ctx, store, strings.Repeat("b", 64)); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(ctx, storagekeys.Video(name), strings.NewReader("foreign cache")); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.BuildNow(ctx, 42); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("build on replacement volume: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "foreign cache" {
		t.Fatalf("concat overwrote foreign cache: %q, %v", data, err)
	}
	assertPlaybackFiles(t, svc, store, name)
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Fatalf("refused publication became terminal: %+v", repo.asset)
	}
	if entries, err := os.ReadDir(svc.store.Scratch().Root()); err != nil || len(entries) != 0 {
		t.Fatalf("refused build left temporary output: %v, %v", entries, err)
	}
	if err := os.RemoveAll(store.Root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(detached, store.Root); err != nil {
		t.Fatal(err)
	}
	runner.beforeWrite = nil
	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("retry after restoring expected volume: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(store.Root, storagekeys.Video(*repo.asset.Filename))); err != nil || string(data) != string(runner.body) {
		t.Fatalf("retry did not publish cache: %q, %v", data, err)
	}
	if repo.asset.Status != repository.PlaybackAssetStatusReady {
		t.Fatalf("retry did not complete: %+v", repo.asset)
	}
	if repo.asset.SizeBytes == nil || *repo.asset.SizeBytes != int64(len(runner.body)) {
		t.Fatalf("published cache has incorrect byte size: %+v", repo.asset)
	}
	if entries, err := os.ReadDir(svc.store.Scratch().Root()); err != nil || len(entries) != 0 {
		t.Fatalf("successful build left temporary output: %v, %v", entries, err)
	}
}

type storageAfterDelete struct {
	storage.Storage
	afterDelete func()
}

func (s storageAfterDelete) Delete(ctx context.Context, path string) error {
	if err := s.Storage.Delete(ctx, path); err != nil {
		return err
	}
	s.afterDelete()
	return nil
}

func TestCachePruneRechecksStorageBetweenEvictions(t *testing.T) {
	var gateErr error
	svc, repo, store, _ := cacheFixture(t, gateFunc(func() error { return gateErr }))
	svc.store = cacheMedia(t, repo, storageAfterDelete{Storage: store, afterDelete: func() { gateErr = storage.ErrUnattached }}, gateFunc(func() error { return gateErr }), nil)
	svc.capacityOverride = func(int64) (int64, bool) { return 0, true }
	size := int64(5)
	for i, name := range []string{"old-cache.mp4", "new-cache.mp4"} {
		if err := store.Save(t.Context(), storagekeys.Video(name), strings.NewReader("cache")); err != nil {
			t.Fatal(err)
		}
		repo.ready = append(repo.ready, repository.VideoPlaybackAsset{VideoID: int64(i + 1), Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size})
	}
	if err := svc.Prune(t.Context()); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("prune error=%v, want storage outage", err)
	}
	if len(repo.ready) != 2 {
		t.Fatalf("eviction lost a reference despite storage changing during deletion: %+v", repo.ready)
	}
	if exists, err := store.Exists(t.Context(), storagekeys.Video("new-cache.mp4")); err != nil || !exists {
		t.Errorf("next cache file lost: exists=%v err=%v", exists, err)
	}
	gateErr = nil
	svc.store = cacheMedia(t, repo, store, nil, nil)
	if err := svc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(repo.ready) != 0 {
		t.Fatalf("retry did not finish both evictions: %+v", repo.ready)
	}
}
