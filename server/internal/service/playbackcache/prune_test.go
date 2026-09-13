package playbackcache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type refusingDeleteStorage struct {
	storage.Storage
	err error
}

func (s refusingDeleteStorage) Delete(context.Context, string) error { return s.err }

func TestCachePruneRetriesFailedObjectDeletionAfterRestart(t *testing.T) {
	for _, failure := range []error{storage.ErrUnattached, storage.ErrUnreachable, errors.New("delete: I/O error")} {
		t.Run(failure.Error(), func(t *testing.T) {
			svc, repo, store, _ := cacheFixture(t, nil)
			name := "vod-42-playback.mp4"
			size := int64(5)
			if err := store.Save(t.Context(), storagekeys.Video(name), strings.NewReader("cache")); err != nil {
				t.Fatal(err)
			}
			repo.ready = []repository.VideoPlaybackAsset{{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size}}
			svc.store = cacheMedia(t, repo, refusingDeleteStorage{Storage: store, err: failure}, nil, nil)
			svc.capacityOverride = func(int64) (int64, bool) { return 0, true }
			if err := svc.Reconcile(t.Context()); !errors.Is(err, failure) {
				t.Fatalf("reconcile error = %v, want %v", err, failure)
			}
			if len(repo.ready) != 1 || svc.currentCacheBytes(t.Context()) != size {
				t.Fatalf("failed deletion lost durable file bookkeeping: %+v", repo.ready)
			}
			if exists, err := store.Exists(t.Context(), storagekeys.Video(name)); err != nil || !exists {
				t.Fatalf("failed delete changed the file: exists=%v err=%v", exists, err)
			}
			// A new service has no in-memory retry state. The surviving row must
			// suffice to reclaim the file once storage is healthy again.
			resumed := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
			t.Cleanup(resumed.Close)
			resumed.capacityOverride = func(int64) (int64, bool) { return 0, true }
			if err := resumed.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(repo.ready) != 0 {
				t.Fatalf("successful retry retained cache row: %+v", repo.ready)
			}
			if exists, err := store.Exists(t.Context(), storagekeys.Video(name)); err != nil || exists {
				t.Fatalf("successful retry left an orphan: exists=%v err=%v", exists, err)
			}
		})
	}
}

// TestPruneEvictsActiveCacheUnderDiskPressure makes an enabled cache yield all
// its bytes when recording writes exhaust free space.
func TestPruneEvictsActiveCacheUnderDiskPressure(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	name := "rec-playback.mp4"
	if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("artifact"))); err != nil {
		t.Fatalf("save: %v", err)
	}
	size := int64(len("artifact"))
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 10},
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size, LastAccessedAt: ptrTime(time.Now())},
		},
	}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.capacityOverride = func(int64) (int64, bool) { return 0, true } // active but no room

	if err := svc.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repo.ready) != 0 {
		t.Fatalf("ready = %#v, want empty (cache yields to recordings)", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(name)); exists {
		t.Fatal("artifact not evicted under disk pressure")
	}
}

// TestPruneStopsOnRowDeleteFailure prevents a failed oldest-entry deletion from
// evicting newer entries instead.
func TestPruneStopsOnRowDeleteFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	oldName, newName := "old-playback.mp4", "new-playback.mp4"
	for _, n := range []string{oldName, newName} {
		if err := store.Save(ctx, storagekeys.Video(n), bytes.NewReader([]byte("artifact"))); err != nil {
			t.Fatalf("save %s: %v", n, err)
		}
	}
	size := int64(len("artifact"))
	now := time.Now()
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 10},
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &oldName, SizeBytes: &size, LastAccessedAt: ptrTime(now.Add(-time.Hour))},
			{VideoID: 2, Status: repository.PlaybackAssetStatusReady, Filename: &newName, SizeBytes: &size, LastAccessedAt: ptrTime(now)},
		},
		deleteAssetErr:      errors.New("db unavailable"),
		deleteAssetErrForID: 1,
	}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.capacityOverride = func(int64) (int64, bool) { return size, true } // budget fits one; total is two

	if err := svc.Prune(ctx); !errors.Is(err, repo.deleteAssetErr) {
		t.Fatalf("Prune: %v, want the database deletion error", err)
	}
	// Keep both rows until the older one's bookkeeping can be cleared. Its
	// file was deleted first; no newer artifact is evicted to compensate.
	if len(repo.ready) != 2 {
		t.Fatalf("ready = %#v, want both kept (no LRU inversion)", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(newName)); !exists {
		t.Fatal("newer artifact evicted to compensate for the un-deletable older one (LRU inversion)")
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(oldName)); exists {
		t.Fatal("old artifact must be deleted before its bookkeeping")
	}
	repo.deleteAssetErr = nil
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(repo.ready) != 1 || repo.ready[0].VideoID != 2 {
		t.Fatalf("retry did not converge in LRU order: %+v", repo.ready)
	}
}

func TestPruningCrossesFullPageWithoutSkippingReadyAssets(t *testing.T) {
	s, repo, raw, _, _ := publicationFixture(t)
	ctx := t.Context()
	var keys []string
	at, size := time.Now().UTC(), int64(1)
	duration, mime := float64(1), "video/mp4"
	for i := range 103 {
		name := fmt.Sprintf("cached-%03d", i)
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: name, Filename: name, BroadcasterID: "b", Status: repository.VideoStatusDone})
		if err != nil {
			t.Fatal(err)
		}
		file := name + ".mp4"
		key := "videos/" + file
		owned, err := s.store.Lock(ctx, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		err = owned.Save(ctx, key, strings.NewReader("m"))
		owned.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{VideoID: v.ID, Status: repository.PlaybackAssetStatusReady, Filename: &file, SizeBytes: &size, GeneratedAt: &at, LastAccessedAt: &at, DurationSeconds: &duration, MimeType: &mime}); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	pages := 0
	repo.afterListReady = func() { pages++ }
	s.capacityOverride = func(int64) (int64, bool) { return 0, true }
	if err := s.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if pages != 2 {
		t.Fatalf("prune discovery pages=%d, want two bounded pages", pages)
	}
	if total, err := repo.SumReadyPlaybackBytes(ctx); err != nil || total != 0 {
		t.Fatalf("prune stopped at full page: bytes=%d err=%v", total, err)
	}
	for _, key := range keys {
		if exists, err := raw.Exists(ctx, key); err != nil || exists {
			t.Fatalf("unpruned object %s: %v %v", key, exists, err)
		}
		if _, err := repo.GetMediaPublication(ctx, key); err == nil {
			t.Fatalf("unpruned journal %s", key)
		}
	}
}

func TestPruneEvictsReadyArtifactsByLRU(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	oldName := "old-playback.mp4"
	newName := "new-playback.mp4"
	for _, name := range []string{oldName, newName} {
		if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("artifact"))); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	size := int64(len("artifact"))
	now := time.Now()
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 10},
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &oldName, SizeBytes: &size, LastAccessedAt: ptrTime(now.Add(-time.Hour))},
			{VideoID: 2, Status: repository.PlaybackAssetStatusReady, Filename: &newName, SizeBytes: &size, LastAccessedAt: ptrTime(now)},
		},
	}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.capacityOverride = func(int64) (int64, bool) { return size, true }

	if err := svc.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repo.ready) != 1 || repo.ready[0].VideoID != 2 {
		t.Fatalf("remaining ready entries = %#v, want only video 2", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(oldName)); exists {
		t.Fatalf("%s still exists after prune", oldName)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(newName)); !exists {
		t.Fatalf("%s evicted but should have been kept", newName)
	}
}

func TestPruneLeavesCacheWhenDisabled(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	name := "keep-playback.mp4"
	if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("artifact"))); err != nil {
		t.Fatalf("save: %v", err)
	}
	size := int64(len("artifact"))
	repo := &fakeRepo{
		// maxPercent=0 means "off": disabling must not wipe the cache.
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 0},
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size, LastAccessedAt: ptrTime(time.Now())},
		},
	}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.capacityOverride = func(int64) (int64, bool) { return 0, true }

	if err := svc.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repo.ready) != 1 {
		t.Fatalf("ready entries = %#v, want cache untouched", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(name)); !exists {
		t.Fatal("artifact wiped while cache disabled")
	}
}
