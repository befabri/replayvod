package video

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/go-chi/chi/v5"
)

const signTestSecret = "stream-signing-secret"

// signedRepo implements only the two repository methods streamSignedPart uses;
// embedding the interface gives the rest nil bodies (never called here).
type signedRepo struct {
	repository.Repository
	video    *repository.Video
	videoErr error
	parts    []repository.VideoPart
}

func (r *signedRepo) GetVideo(_ context.Context, _ int64) (*repository.Video, error) {
	return r.video, r.videoErr
}
func (r *signedRepo) ListVideoParts(_ context.Context, _ int64) ([]repository.VideoPart, error) {
	return r.parts, nil
}

// signedStorage serves canned bytes for any path.
type signedStorage struct {
	storage.Storage
	body []byte
}

type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

func (s *signedStorage) Open(_ context.Context, _ string) (io.ReadSeekCloser, error) {
	return nopSeekCloser{bytes.NewReader(s.body)}, nil
}
func (s *signedStorage) Stat(_ context.Context, _ string) (storage.FileInfo, error) {
	return storage.FileInfo{Size: int64(len(s.body)), ModTime: time.Unix(1000, 0)}, nil
}

// signedTestServer wires the signed route on a chi router backed by the fakes.
func signedTestServer(t *testing.T, repo repository.Repository) *httptest.Server {
	t.Helper()
	h := NewStreamHandler(repo, &signedStorage{body: []byte("video-bytes")}, videodownload.NewVerifier(signTestSecret), testClientLogger())
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { h.SetupSignedRoutes(r) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// getSigned mints a signed URL with the given signer and issues it against srv,
// rewriting only the origin so the path+query (exp+sig) are exactly as signed.
func getSigned(t *testing.T, srv *httptest.Server, signer *videodownload.Signer, videoID int64, part int32) *http.Response {
	t.Helper()
	signed, err := url.Parse(signer.PartURL(videoID, part))
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	resp, err := http.Get(srv.URL + signed.Path + "?" + signed.RawQuery)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	return resp
}

func doneVideo() *repository.Video {
	return &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"}
}

func TestStreamSignedPart_validSignatureServesPart(t *testing.T) {
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4"},
			{PartIndex: 2, Filename: "vod-42-02.mp4"},
		},
	}
	srv := signedTestServer(t, repo)
	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)

	// Part 2 (not just the first part) must be reachable — the whole point of per-part URLs.
	resp := getSigned(t, srv, signer, 42, 2)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "video-bytes" {
		t.Fatalf("body = %q", body)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Fatal("expected an attachment Content-Disposition")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("content-type = %q, want video/mp4", ct)
	}
}

func TestStreamSignedPart_audioPartUsesAudioContentType(t *testing.T) {
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.m4a"}},
	}
	srv := signedTestServer(t, repo)
	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)

	resp := getSigned(t, srv, signer, 42, 1)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/mp4" {
		t.Fatalf("content-type = %q, want audio/mp4", ct)
	}
}

func TestStreamSignedPart_badSignatureIs403(t *testing.T) {
	srv := signedTestServer(t, &signedRepo{video: doneVideo(), parts: []repository.VideoPart{{PartIndex: 1, Filename: "p.mp4"}}})

	// A URL signed with a DIFFERENT secret must not verify.
	wrong := videodownload.NewSigner("not-the-secret", "https://app.example", time.Hour)
	resp := getSigned(t, srv, wrong, 42, 1)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestStreamSignedPart_deletedIs410(t *testing.T) {
	deleted := doneVideo()
	when := time.Unix(2000, 0)
	deleted.DeletedAt = &when
	srv := signedTestServer(t, &signedRepo{video: deleted, parts: []repository.VideoPart{{PartIndex: 1, Filename: "p.mp4"}}})

	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)
	resp := getSigned(t, srv, signer, 42, 1)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("deleted video status = %d, want 410", resp.StatusCode)
	}
}

func TestStreamSignedPart_unknownPartIs404(t *testing.T) {
	srv := signedTestServer(t, &signedRepo{video: doneVideo(), parts: []repository.VideoPart{{PartIndex: 1, Filename: "p.mp4"}}})

	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)
	resp := getSigned(t, srv, signer, 42, 9) // no such part
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown part status = %d, want 404", resp.StatusCode)
	}
}

func TestPartPath(t *testing.T) {
	v := &repository.Video{ID: 42, Filename: "vod-42"}
	parts := []repository.VideoPart{
		{PartIndex: 1, Filename: "vod-42-01.mp4"},
		{PartIndex: 2, Filename: "vod-42-02.mp4"},
	}

	if rel, name, ok := partPath(v, parts, 2); !ok || rel != "videos/vod-42-02.mp4" || name != "vod-42-02.mp4" {
		t.Fatalf("part 2 = (%q,%q,%v)", rel, name, ok)
	}
	if _, _, ok := partPath(v, parts, 5); ok {
		t.Fatal("unknown index should not resolve")
	}
	// Legacy single-file row (no parts): only index 0 resolves, to filename.mp4.
	if rel, name, ok := partPath(v, nil, 0); !ok || rel != "videos/vod-42.mp4" || name != "vod-42.mp4" {
		t.Fatalf("legacy index 0 = (%q,%q,%v)", rel, name, ok)
	}
	if _, _, ok := partPath(v, nil, 1); ok {
		t.Fatal("legacy row has no index 1")
	}
}
