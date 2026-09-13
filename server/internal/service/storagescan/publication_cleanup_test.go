package storagescan

import (
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

func TestPublicationCleanupPreservesMissingRecordingUntilRestoration(t *testing.T) {
	for _, returnBeforeCleanup := range []bool{false, true} {
		name := "media still missing"
		if returnBeforeCleanup {
			name = "media returned before restoration"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			v := f.seed(t, "returned-publication", 1)
			keys := []string{
				"videos/" + v.Filename + "-part01.mp4",
				"thumbnails/" + v.Filename + "-part01.jpg",
				"thumbnails/" + v.Filename + "-part01-strip.jpg",
			}
			store := mediatest.New(t, f.repo, f.store, f.mon, nil)
			owned, err := store.Lock(f.ctx, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			func() {
				defer owned.Close()
				for _, key := range keys {
					if err := owned.Save(f.ctx, key, strings.NewReader("data")); err != nil {
						t.Fatal(err)
					}
				}
			}()
			if err := f.store.Delete(f.ctx, keys[0]); err != nil {
				t.Fatal(err)
			}
			if changed, err := f.svc.MarkMissing(f.ctx, v.ID); err != nil || !changed {
				t.Fatalf("mark missing = %v, %v", changed, err)
			}
			f.assertTombstonedMissing(t, v.ID)
			if returnBeforeCleanup {
				f.save(t, keys[0])
			}
			// The cleanup obligation must survive recreation of the service.
			store = mediatest.New(t, f.repo, f.store, f.mon, nil)
			if err := store.Reconcile(f.ctx); err != nil {
				t.Fatal(err)
			}
			f.assertPreviewsKept(t, v.Filename)
			assertPublicationsKept(t, store, f, v.ID, keys)
			if !returnBeforeCleanup {
				f.save(t, keys[0])
			}
			if err := f.svc.Restore(f.ctx, v.ID); err != nil {
				t.Fatalf("returned media could not be restored: %v", err)
			}
			f.assertLive(t, v.ID)
			if !f.exists(t, keys[0]) {
				t.Fatal("cleanup deleted returned source media")
			}
		})
	}
}

func assertPublicationsKept(t *testing.T, store *mediastore.Store, f fixture, videoID int64, keys []string) {
	t.Helper()
	for _, key := range keys {
		row, err := store.Publication(f.ctx, key)
		if err != nil || row.VideoID != videoID || row.DeleteRequested {
			t.Fatalf("missing tombstone lost publication %s: %+v, %v", key, row, err)
		}
	}
	rows, err := f.repo.ListRecordingPublications(f.ctx, videoID, "", 100)
	if err != nil || len(rows) != len(keys) {
		t.Fatalf("publications = %+v, %v", rows, err)
	}
	if v, err := f.repo.GetVideo(f.ctx, videoID); err != nil || v.DeletionKind == nil || *v.DeletionKind != repository.DeletionKindMissing {
		t.Fatalf("cleanup changed missing tombstone: %+v, %v", v, err)
	}
}
