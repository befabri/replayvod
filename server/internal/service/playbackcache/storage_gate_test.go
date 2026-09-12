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
)

type gateFunc func() error

func (f gateFunc) Ready() error                 { return f() }
func (f gateFunc) Verify(context.Context) error { return f() }

func cacheStorageFixture(t *testing.T, gate StorageGate) (*Service, *fakeRepo, *storage.LocalStorage, *fakeRunner) {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fps := 60.0
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 10, SizeBytes: 4},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 12, SizeBytes: 4},
		},
	}
	for _, part := range repo.parts {
		if err := store.Save(t.Context(), storagekeys.Video(part.Filename), strings.NewReader("part")); err != nil {
			t.Fatal(err)
		}
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, gate, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	t.Cleanup(svc.Close)
	return svc, repo, store, runner
}

func TestCacheRefusesUnavailableStorage(t *testing.T) {
	for _, verdict := range []struct {
		name string
		err  error
	}{
		{"unattached", storage.ErrUnattached}, {"unreachable", storage.ErrUnreachable}, {"read-only", storage.ErrReadOnly},
	} {
		t.Run(verdict.name, func(t *testing.T) {
			svc, repo, store, runner := cacheStorageFixture(t, gateFunc(func() error { return verdict.err }))
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
			svc.deleteArtifact(ctx, name)
			if exists, err := store.Exists(ctx, storagekeys.Video(name)); err != nil || !exists {
				t.Errorf("cleanup deleted existing cache: exists=%v err=%v", exists, err)
			}
		})
	}
}

func TestCacheReconcilePreservesForeignStorage(t *testing.T) {
	svc, repo, store, _ := cacheStorageFixture(t, nil)
	ctx := t.Context()
	expected := strings.Repeat("a", 64)
	if err := storage.WriteMarker(ctx, store, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	svc.gate = gateFunc(func() error { return storage.Ready(ctx, store, expected) })
	if err := svc.gate.Verify(ctx); !errors.Is(err, storage.ErrUnattached) {
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
	svc, repo, store, _ := cacheStorageFixture(t, gateFunc(func() error { return gateErr }))
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
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Errorf("storage outage recorded a terminal cache verdict: %+v", repo.asset)
	}
}

func TestCacheBuildRechecksStorageBeforeUpload(t *testing.T) {
	var gateErr error
	svc, repo, store, runner := cacheStorageFixture(t, gateFunc(func() error { return gateErr }))
	// Expose only the object-storage interface, so concatenation uses scratch
	// and then Save, as it does for S3.
	svc.store = struct{ storage.Storage }{store}
	svc.capacityOverride = func(int64) (int64, bool) { return 1000, true }
	runner.beforeWrite = func() { gateErr = storage.ErrReadOnly }
	if err := svc.BuildNow(t.Context(), 42); !errors.Is(err, storage.ErrReadOnly) {
		t.Fatalf("storage change during concat: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("concat never ran: %d calls", runner.calls)
	}
	if exists, err := store.Exists(t.Context(), storagekeys.Video("vod-42-playback.mp4")); err != nil || exists {
		t.Errorf("uploaded onto read-only storage: exists=%v err=%v", exists, err)
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Errorf("interrupted upload left a terminal verdict: %+v", repo.asset)
	}
}

func TestCacheBuildPreservesReplacementVolumeDuringConcat(t *testing.T) {
	svc, repo, store, runner := cacheStorageFixture(t, nil)
	ctx := t.Context()
	expected := strings.Repeat("a", 64)
	if err := storage.WriteMarker(ctx, store, expected); err != nil {
		t.Fatal(err)
	}
	svc.gate = gateFunc(func() error { return storage.Ready(ctx, store, expected) })
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
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Fatalf("refused publication became terminal: %+v", repo.asset)
	}
	if entries, err := os.ReadDir(svc.scratch); err != nil || len(entries) != 0 {
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
	if data, err := os.ReadFile(path); err != nil || string(data) != string(runner.body) {
		t.Fatalf("retry did not publish cache: %q, %v", data, err)
	}
	if repo.asset.Status != repository.PlaybackAssetStatusReady {
		t.Fatalf("retry did not complete: %+v", repo.asset)
	}
	if repo.asset.SizeBytes == nil || *repo.asset.SizeBytes != int64(len(runner.body)) {
		t.Fatalf("published cache has incorrect byte size: %+v", repo.asset)
	}
	if entries, err := os.ReadDir(svc.scratch); err != nil || len(entries) != 0 {
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
	svc, repo, store, _ := cacheStorageFixture(t, gateFunc(func() error { return gateErr }))
	svc.store = storageAfterDelete{Storage: store, afterDelete: func() { gateErr = storage.ErrUnattached }}
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
	svc.store = store
	if err := svc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(repo.ready) != 0 {
		t.Fatalf("retry did not finish both evictions: %+v", repo.ready)
	}
}
