package mediastore

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

type publicationSnapshotHook struct {
	repository.Repository
	afterList func()
}

func (r *publicationSnapshotHook) ListMediaPublications(ctx context.Context, after string, limit int) ([]repository.MediaPublication, error) {
	rows, err := r.Repository.ListMediaPublications(ctx, after, limit)
	if err == nil && r.afterList != nil {
		hook := r.afterList
		r.afterList = nil
		hook()
	}
	return rows, err
}

func TestReconcileDoesNotDeletePublicationReplacedAfterDiscovery(t *testing.T) {
	for _, changedOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "same recording", true: "different recording"}[changedOwner], func(t *testing.T) {
			repo, raw, video := mediaFixture(t)
			ctx := t.Context()
			observed := &publicationSnapshotHook{Repository: repo}
			store := managed(t, observed, raw)
			key := "videos/reused.mp4"
			owner, err := store.Lock(ctx, video.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.Save(ctx, key, strings.NewReader("old bytes")); err != nil {
				t.Fatal(err)
			}
			owner.Close()
			if err := repo.RequestMediaPublicationDelete(ctx, key); err != nil {
				t.Fatal(err)
			}
			nextVideo := video
			if changedOwner {
				nextVideo, err = repo.CreateVideo(ctx, &repository.VideoInput{
					JobID: "replacement", Filename: "replacement", BroadcasterID: video.BroadcasterID,
					Status: repository.VideoStatusDone,
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			observed.afterList = func() {
				previous, err := store.Lock(ctx, video.ID)
				if err != nil {
					t.Fatal(err)
				}
				err = previous.Delete(ctx, key)
				previous.Close()
				if err != nil {
					t.Fatal(err)
				}
				next, err := store.Lock(ctx, nextVideo.ID)
				if err != nil {
					t.Fatal(err)
				}
				err = next.Save(ctx, key, strings.NewReader("replacement bytes"))
				next.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			publication, err := repo.GetMediaPublication(ctx, key)
			if err != nil || publication.VideoID != nextVideo.ID || publication.DeleteRequested || publication.Unresolved {
				t.Fatalf("stale discovery changed replacement journal: %+v, %v", publication, err)
			}
			f, err := raw.Open(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			body, err := io.ReadAll(f)
			if err != nil || string(body) != "replacement bytes" {
				t.Fatalf("replacement media changed: %q, %v", body, err)
			}
		})
	}
}
