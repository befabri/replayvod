package playbackcache

import (
	"context"
	"errors"
	"strings"
	"testing"

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
			svc, repo, store, _ := cacheStorageFixture(t, nil)
			name := "vod-42-playback.mp4"
			size := int64(5)
			if err := store.Save(t.Context(), storagekeys.Video(name), strings.NewReader("cache")); err != nil {
				t.Fatal(err)
			}
			repo.ready = []repository.VideoPlaybackAsset{{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size}}
			svc.store = refusingDeleteStorage{Storage: store, err: failure}
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
			resumed := New(repo, store, nil, t.TempDir(), "", nil)
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
