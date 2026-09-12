package video

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/go-chi/chi/v5"
)

func TestExistingMediaHonorsStorageReadiness(t *testing.T) {
	signer := videodownload.NewSigner(signTestSecret, "http://example.com", time.Hour)
	for _, verdict := range []struct {
		name string
		err  error
	}{
		{"attached", nil}, {"read-only", storage.ErrReadOnly}, {"full", storage.ErrFull},
		{"unattached", storage.ErrUnattached}, {"unreachable", storage.ErrUnreachable},
	} {
		for _, route := range []struct{ name, url string }{
			{"video", "/api/v1/videos/7/stream"},
			{"part", "/api/v1/videos/7/parts/1/stream"},
			{"signed part", signer.PartURL(7, 1)},
			{"playback", "/api/v1/videos/7/playback/stream"},
			{"thumbnail", "/api/v1/thumbnails/rec.jpg"},
		} {
			for _, method := range []string{http.MethodGet, http.MethodHead, "range"} {
				if route.name == "thumbnail" && method == http.MethodHead {
					continue // Thumbnails only register GET.
				}
				t.Run(verdict.name+"/"+route.name+"/"+method, func(t *testing.T) {
					assetName := "playback.mp4"
					repo := missingPartRepo()
					repo.asset = &repository.VideoPlaybackAsset{Status: repository.PlaybackAssetStatusReady, Filename: &assetName}
					store := &signedStorage{body: []byte("existing media bytes")}
					marker := newBlockingMarker(true)
					close(marker.release)
					h := NewStreamHandler(repo, store, videodownload.NewVerifier(signTestSecret), testClientLogger(),
						WithStorageGate(gateFunc(func() error { return verdict.err })), WithMissingMarker(marker))
					router := chi.NewRouter()
					router.Route("/api/v1", func(r chi.Router) {
						h.SetupRoutes(r, func(next http.Handler) http.Handler { return next })
						h.SetupSignedRoutes(r)
					})
					httpMethod := method
					if method == "range" {
						httpMethod = http.MethodGet
					}
					req := httptest.NewRequest(httpMethod, route.url, nil)
					want := http.StatusOK
					if method == "range" {
						req.Header.Set("Range", "bytes=0-3")
						want = http.StatusPartialContent
					}
					if !storage.CanRead(verdict.err) {
						want = http.StatusServiceUnavailable
					}
					resp := httptest.NewRecorder()
					router.ServeHTTP(resp, req)
					if resp.Code != want {
						t.Fatalf("status=%d want=%d body=%s", resp.Code, want, resp.Body.String())
					}
					if want == http.StatusServiceUnavailable && (store.stats != 0 || len(store.opened) != 0 || repo.touches != 0) {
						t.Fatalf("untrusted media accessed: stats=%d opens=%v touches=%d", store.stats, store.opened, repo.touches)
					}
					if marker.callCount() != 0 || repo.deletes != 0 {
						t.Fatal("readiness refusal reconciled existing media")
					}
				})
			}
		}
	}
}
