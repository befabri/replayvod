package archiveposter

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type storageAfterSave struct {
	storage.Storage
	afterSave func()
}

func (s storageAfterSave) Save(ctx context.Context, path string, body io.Reader) error {
	if err := s.Storage.Save(ctx, path, body); err != nil {
		return err
	}
	s.afterSave()
	return nil
}

func TestFetchStorageLossDuringSaveLeavesPosterRetryable(t *testing.T) {
	repo, store := posterFixture(t)
	v := seedArchive(t, repo, "storage-loss", "123")
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "poster")
	}))
	defer cdn.Close()
	var unavailable bool
	gate := readyFunc(func(context.Context) error {
		if unavailable {
			return storage.ErrUnattached
		}
		return nil
	})
	writes := 0
	swapping := storageAfterSave{Storage: store, afterSave: func() {
		writes++
		unavailable = writes == 1
	}}
	posters := NewStore(repo, mediatest.New(t, repo, swapping, gate, nil), cdn.Client(), slog.New(slog.DiscardHandler))
	if posters.Fetch(t.Context(), v.ID, v.Filename, cdn.URL) {
		t.Fatal("published poster after storage disappeared during Save")
	}
	row, err := repo.GetVideo(t.Context(), v.ID)
	if err != nil || row.Thumbnail != nil {
		t.Fatalf("lost storage left a thumbnail reference: %+v %v", row, err)
	}
	unavailable = false
	if !posters.Fetch(t.Context(), v.ID, v.Filename, cdn.URL) || writes != 2 {
		t.Fatalf("poster did not rewrite after recovery: writes=%d", writes)
	}
}

type delayedPosterUpload struct {
	storage.Storage
	key, body string
}

func (s *delayedPosterUpload) Save(ctx context.Context, key string, body io.Reader) error {
	if s.key == "" {
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		s.key, s.body = key, string(data)
		return context.DeadlineExceeded
	}
	return s.Storage.Save(ctx, key, body)
}

func TestFetchRetriesChangedImageWithoutReusingUncertainUploadKey(t *testing.T) {
	repo, raw := posterFixture(t)
	v := seedArchive(t, repo, "changed-poster", "123")
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer cdn.Close()
	remote := &delayedPosterUpload{Storage: raw}
	posters := NewStore(repo, mediatest.New(t, repo, remote, nil, nil), cdn.Client(), slog.New(slog.DiscardHandler))
	ctx := t.Context()
	if posters.Fetch(ctx, v.ID, v.Filename, cdn.URL+"/earlier") {
		t.Fatal("uncertain upload was published")
	}
	if !posters.Fetch(ctx, v.ID, v.Filename, cdn.URL+"/current") {
		t.Fatal("changed CDN image could not be retried")
	}
	fresh, err := repo.GetVideo(ctx, v.ID)
	if err != nil || fresh.Thumbnail == nil || *fresh.Thumbnail != storagekeys.Snapshot(v.Filename, 1) {
		t.Fatalf("retry reference: %+v, %v", fresh, err)
	}
	if err := raw.Save(ctx, remote.key, strings.NewReader(remote.body)); err != nil {
		t.Fatal(err)
	}
	file, err := raw.Open(ctx, *fresh.Thumbnail)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil || string(body) != "/current" {
		t.Fatalf("late upload replaced current poster: %q, %v", body, err)
	}
	publication, err := repo.GetMediaPublication(ctx, remote.key)
	if err != nil || !publication.Unresolved {
		t.Fatalf("late upload lost its cleanup record: %+v, %v", publication, err)
	}
}
