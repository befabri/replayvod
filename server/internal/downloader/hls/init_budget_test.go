package hls

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/befabri/replayvod/server/internal/storage"
)

func TestFMP4InitCannotBypassScratchWriteBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "fmp4 initialization") }))
	defer srv.Close()
	fetcher := NewFetcher(srv.Client(), FetcherConfig{TransportAttempts: 1, ServerErrorAttempts: 1}, slog.New(slog.DiscardHandler))
	dir := t.TempDir()
	writes := 0
	err := fetchInit(t.Context(), fetcher, dir, srv.URL, writeFileFunc(func(context.Context, *os.File, []byte) (int, error) { writes++; return 0, storage.ErrFull }))
	if !errors.Is(err, storage.ErrFull) || writes == 0 {
		t.Fatalf("init escaped write accounting: writes=%d err=%v", writes, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("rejected init left output or staging bytes: %+v %v", files, err)
	}
}
