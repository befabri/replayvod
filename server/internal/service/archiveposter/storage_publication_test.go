package archiveposter

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/befabri/replayvod/server/internal/storage"
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
	posters := NewStore(repo, swapping, gate, cdn.Client(), slog.New(slog.DiscardHandler))
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
