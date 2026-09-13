package mediastore

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

type journalBoundaryHook struct {
	repository.Repository
	afterWrite func()
}

func (r journalBoundaryHook) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	err := r.Repository.WithTx(ctx, fn)
	if err == nil && r.afterWrite != nil {
		r.afterWrite()
	}
	return err
}

func (r journalBoundaryHook) RequestMediaPublicationDelete(ctx context.Context, key string) error {
	err := r.Repository.RequestMediaPublicationDelete(ctx, key)
	if err == nil && r.afterWrite != nil {
		r.afterWrite()
	}
	return err
}

func TestMediaEffectsRecheckStorageAfterJournalWrite(t *testing.T) {
	for _, operation := range []string{"save", "delete"} {
		t.Run(operation, func(t *testing.T) {
			repo, raw, video := mediaFixture(t)
			ctx := t.Context()
			key := "videos/protected.mp4"
			if operation == "delete" {
				if err := raw.Save(ctx, key, strings.NewReader("foreign media")); err != nil {
					t.Fatal(err)
				}
			}
			var verdict error
			hooked := journalBoundaryHook{Repository: repo, afterWrite: func() { verdict = storage.ErrUnattached }}
			store := New(hooked, raw, testGate(func(context.Context) error { return verdict }), &recordinglock.Locks{}, t.TempDir())
			owned, err := store.Lock(ctx, video.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			if operation == "save" {
				err = owned.Save(ctx, key, strings.NewReader("new bytes"))
			} else {
				err = owned.Delete(ctx, key)
			}
			if !errors.Is(err, storage.ErrUnattached) {
				t.Fatalf("storage change ignored: %v", err)
			}
			if operation == "save" {
				if exists, err := raw.Exists(ctx, key); err != nil || exists {
					t.Fatalf("unattached storage received media: %v, %v", exists, err)
				}
				return
			}
			f, err := raw.Open(ctx, key)
			if err != nil {
				t.Fatalf("unattached storage lost media: %v", err)
			}
			defer f.Close()
			body, err := io.ReadAll(f)
			if err != nil || string(body) != "foreign media" {
				t.Fatalf("unattached media changed: %q, %v", body, err)
			}
		})
	}
}
