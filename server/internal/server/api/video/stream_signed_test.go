package video

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/go-chi/chi/v5"
)

type fakeBuilder struct {
	mu    sync.Mutex
	calls []int64
}

func (f *fakeBuilder) StartBuild(_ context.Context, videoID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, videoID)
	return nil
}

func (f *fakeBuilder) videoIDs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.calls...)
}

const signTestSecret = "stream-signing-secret"

// signedRepo panics on unexpected calls through its embedded interface.
type signedRepo struct {
	repository.Repository
	video    *repository.Video
	videos   map[int64]*repository.Video
	videoErr error
	// videoEntered, when set, receives once GetVideo is reached; the read then
	// waits for the caller to leave and returns its context error.
	videoEntered chan struct{}
	parts        []repository.VideoPart
	partsByVideo map[int64][]repository.VideoPart
	asset        *repository.VideoPlaybackAsset
	assets       map[int64]*repository.VideoPlaybackAsset
	touches      int
	deletes      int
}

func (r *signedRepo) GetVideo(ctx context.Context, id int64) (*repository.Video, error) {
	if r.videoEntered != nil {
		select {
		case r.videoEntered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if r.videoErr != nil {
		return nil, r.videoErr
	}
	if r.videos != nil {
		v, ok := r.videos[id]
		if !ok {
			return nil, repository.ErrNotFound
		}
		return v, nil
	}
	return r.video, r.videoErr
}
func (r *signedRepo) ListVideoParts(_ context.Context, id int64) ([]repository.VideoPart, error) {
	if r.partsByVideo != nil {
		return r.partsByVideo[id], nil
	}
	return r.parts, nil
}
func (r *signedRepo) GetVideoPlaybackAsset(_ context.Context, id int64) (*repository.VideoPlaybackAsset, error) {
	if r.assets != nil {
		asset, ok := r.assets[id]
		if !ok || asset == nil {
			return nil, repository.ErrNotFound
		}
		return asset, nil
	}
	if r.asset == nil {
		return nil, repository.ErrNotFound
	}
	return r.asset, nil
}
func (r *signedRepo) TouchVideoPlaybackAsset(_ context.Context, _ int64) error {
	r.touches++
	return nil
}
func (r *signedRepo) DeleteVideoPlaybackAsset(_ context.Context, _ int64) error {
	r.deletes++
	return nil
}

type signedStorage struct {
	storage.Storage
	body     []byte
	bodies   map[string][]byte
	openErrs map[string][]error
	statErrs map[string][]error
	opened   []string
	stats    int // Stat call count (each is a HeadObject on S3)
}

type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

func (s *signedStorage) Open(_ context.Context, path string) (io.ReadSeekCloser, error) {
	s.opened = append(s.opened, path)
	if errs := s.openErrs[path]; len(errs) > 0 {
		err := errs[0]
		s.openErrs[path] = errs[1:]
		return nil, err
	}
	body := s.body
	if s.bodies != nil {
		b, ok := s.bodies[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		body = b
	}
	return nopSeekCloser{bytes.NewReader(body)}, nil
}
func (s *signedStorage) Save(_ context.Context, path string, r io.Reader) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if s.bodies == nil {
		s.bodies = make(map[string][]byte)
	}
	s.bodies[path] = body
	return nil
}
func (s *signedStorage) Stat(_ context.Context, path string) (storage.FileInfo, error) {
	s.stats++
	if errs := s.statErrs[path]; len(errs) > 0 {
		err := errs[0]
		s.statErrs[path] = errs[1:]
		return storage.FileInfo{}, err
	}
	body := s.body
	if s.bodies != nil {
		b, ok := s.bodies[path]
		if !ok {
			return storage.FileInfo{}, os.ErrNotExist
		}
		body = b
	}
	return storage.FileInfo{Size: int64(len(body)), ModTime: time.Unix(1000, 0)}, nil
}

func signedTestServer(t *testing.T, repo repository.Repository) *httptest.Server {
	t.Helper()
	return signedRouteTestServer(t, repo, &signedStorage{body: []byte("video-bytes")})
}

func signedRouteTestServer(t *testing.T, repo repository.Repository, store storage.Storage) *httptest.Server {
	t.Helper()
	h := NewStreamHandler(repo, streamMedia(t, repo, store, nil, nil), videodownload.NewVerifier(signTestSecret), testClientLogger())
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { h.SetupSignedRoutes(r) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func sessionPartTestServer(t *testing.T, repo repository.Repository, opts ...StreamHandlerOption) *httptest.Server {
	t.Helper()
	return streamRouteTestServer(t, repo, &signedStorage{body: []byte("video-bytes")}, testClientLogger(), opts...)
}

func playbackTestServer(t *testing.T, repo repository.Repository, store storage.Storage) *httptest.Server {
	t.Helper()
	return streamRouteTestServer(t, repo, store, testClientLogger())
}

func streamRouteTestServer(t *testing.T, repo repository.Repository, store storage.Storage, log *slog.Logger, opts ...StreamHandlerOption) *httptest.Server {
	t.Helper()
	h := NewStreamHandler(repo, streamMedia(t, repo, store, nil, nil), videodownload.NewVerifier(signTestSecret), log, opts...)
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h.SetupRoutes(r, func(next http.Handler) http.Handler { return next })
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// doSigned rewrites only the URL origin, preserving the signed path and query.
func doSigned(t *testing.T, srv *httptest.Server, signer *videodownload.Signer, method string, videoID int64, part int32) *http.Response {
	t.Helper()
	signed, err := url.Parse(signer.PartURL(videoID, part))
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	req, err := http.NewRequest(method, srv.URL+signed.Path+"?"+signed.RawQuery, nil)
	if err != nil {
		t.Fatalf("build %s: %v", method, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return resp
}

func getSigned(t *testing.T, srv *httptest.Server, signer *videodownload.Signer, videoID int64, part int32) *http.Response {
	t.Helper()
	return doSigned(t, srv, signer, http.MethodGet, videoID, part)
}

func headSigned(t *testing.T, srv *httptest.Server, signer *videodownload.Signer, videoID int64, part int32) *http.Response {
	t.Helper()
	return doSigned(t, srv, signer, http.MethodHead, videoID, part)
}

func getSessionPart(t *testing.T, srv *httptest.Server, videoID int64, part int32) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/v1/videos/" + strconv.FormatInt(videoID, 10) + "/parts/" + strconv.FormatInt(int64(part), 10) + "/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	return resp
}

func getPlaybackStream(t *testing.T, srv *httptest.Server, videoID int64) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/v1/videos/" + strconv.FormatInt(videoID, 10) + "/playback/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	return resp
}

func headSessionPart(t *testing.T, srv *httptest.Server, videoID int64, part int32) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodHead, srv.URL+"/api/v1/videos/"+strconv.FormatInt(videoID, 10)+"/parts/"+strconv.FormatInt(int64(part), 10)+"/stream", nil)
	if err != nil {
		t.Fatalf("build HEAD: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	return resp
}

func headSessionStream(t *testing.T, srv *httptest.Server, videoID int64) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodHead, srv.URL+"/api/v1/videos/"+strconv.FormatInt(videoID, 10)+"/parts/1/stream", nil)
	if err != nil {
		t.Fatalf("build HEAD: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	return resp
}

func doneVideo(ids ...int64) *repository.Video {
	id := int64(42)
	if len(ids) > 0 {
		id = ids[0]
	}
	return &repository.Video{ID: id, Status: repository.VideoStatusDone, Filename: "vod-" + strconv.FormatInt(id, 10)}
}

func TestStreamSignedPart_validSignatureServesPart(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{
		"videos/vod-42-02.mp4": []byte("part02-bytes"),
	}}
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4"},
			{PartIndex: 2, Filename: "vod-42-02.mp4"},
		},
	}
	srv := signedRouteTestServer(t, repo, store)
	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)

	resp := getSigned(t, srv, signer, 42, 2)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "part02-bytes" {
		t.Fatalf("body = %q", body)
	}
	if len(store.opened) != 1 || store.opened[0] != "videos/vod-42-02.mp4" {
		t.Fatalf("opened paths = %#v, want part 2 path", store.opened)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Fatal("expected an attachment Content-Disposition")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("content-type = %q, want video/mp4", ct)
	}
}

func TestStreamPart_sessionRouteServesRequestedPartInline(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{
		"videos/vod-42-02.mp4": []byte("part02-bytes"),
	}}
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4"},
			{PartIndex: 2, Filename: "vod-42-02.mp4"},
		},
	}
	srv := streamRouteTestServer(t, repo, store, testClientLogger())

	resp := getSessionPart(t, srv, 42, 2)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "part02-bytes" {
		t.Fatalf("body = %q", body)
	}
	if len(store.opened) != 1 || store.opened[0] != "videos/vod-42-02.mp4" {
		t.Fatalf("opened paths = %#v, want part 2 path", store.opened)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		t.Fatalf("content-disposition = %q, want inline response with no attachment", cd)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("content-type = %q, want video/mp4", ct)
	}
}

func TestStreamPart_headProbeReturnsMediaHeaders(t *testing.T) {
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}},
	}
	srv := sessionPartTestServer(t, repo)

	resp := headSessionPart(t, srv, 42, 1)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Fatalf("HEAD body length = %d, want 0", len(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("content-type = %q, want video/mp4", ct)
	}
	if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("accept-ranges = %q, want bytes", ar)
	}
}

func TestStreamPartOpeningRequestsCanRetryPlaybackBuild(t *testing.T) {
	builder := &fakeBuilder{}
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4"},
			{PartIndex: 2, Filename: "vod-42-02.mp4"},
		},
	}
	srv := sessionPartTestServer(t, repo, WithPlaybackBuilder(builder))

	for _, request := range []struct {
		method, byteRange string
	}{
		{http.MethodHead, ""},
		{http.MethodGet, "bytes=0-"},
		{http.MethodGet, "bytes=4-"},
		{http.MethodGet, "bytes=0-"},
		{http.MethodGet, ""},
	} {
		req, err := http.NewRequestWithContext(t.Context(), request.method, srv.URL+"/api/v1/videos/42/parts/1/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		if request.byteRange != "" {
			req.Header.Set("Range", request.byteRange)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	if got := builder.videoIDs(); len(got) != 3 || got[0] != 42 || got[1] != 42 || got[2] != 42 {
		t.Fatalf("StartBuild calls = %#v, want all three opening GETs and no HEAD/continuation builds", got)
	}
}

func TestStreamSignedPart_doesNotKickBuild(t *testing.T) {
	builder := &fakeBuilder{}
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4"},
			{PartIndex: 2, Filename: "vod-42-02.mp4"},
		},
	}
	store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-01.mp4": []byte("part01")}}
	h := NewStreamHandler(repo, streamMedia(t, repo, store, nil, nil), videodownload.NewVerifier(signTestSecret), testClientLogger(), WithPlaybackBuilder(builder))
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { h.SetupSignedRoutes(r) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)
	resp := getSigned(t, srv, signer, 42, 1)
	resp.Body.Close()

	if n := len(builder.videoIDs()); n != 0 {
		t.Fatalf("signed download kicked %d builds, want 0", n)
	}
}

// TestStreamPart_clientCanceledIsNotServerError treats a Range request the
// browser aborted while the repository was still reading as client
// cancellation, without a server-error status or log.
func TestStreamPart_clientCanceledIsNotServerError(t *testing.T) {
	logs := &capturingHandler{}
	repo := &signedRepo{
		videoEntered: make(chan struct{}, 1),
		parts:        []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}},
	}
	h := NewStreamHandler(repo, streamMedia(t, repo, &signedStorage{body: []byte("video-bytes")}, nil, nil), videodownload.NewVerifier(signTestSecret), slog.New(logs))
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	router := chi.NewRouter()
	h.SetupRoutes(router, func(next http.Handler) http.Handler { return next })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/videos/42/parts/1/stream", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		router.ServeHTTP(response, req)
	}()

	select {
	case <-repo.videoEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("request never reached the repository")
	}
	cancel() // The viewer seeks away while the read is in flight.
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled handler did not exit")
	}

	if response.Code != statusClientClosed {
		t.Fatalf("status = %d, want %d (client closed request)", response.Code, statusClientClosed)
	}
	if n := logs.countAtLeast(slog.LevelWarn); n != 0 {
		t.Fatalf("emitted %d WARN+ log records for a client cancellation, want 0", n)
	}
}

func TestStreamPlayback_ServesReadyArtifact(t *testing.T) {
	fps := 60.0
	artifactName := "vod-42-playback.mp4"
	artifactMime := "video/mp4"
	store := &signedStorage{
		bodies: map[string][]byte{
			"videos/vod-42-playback.mp4": []byte("playback-video-bytes"),
		},
	}
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SizeBytes: 8},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SizeBytes: 8},
		},
		asset: &repository.VideoPlaybackAsset{
			VideoID:  42,
			Status:   repository.PlaybackAssetStatusReady,
			Filename: &artifactName,
			MimeType: &artifactMime,
		},
	}
	srv := playbackTestServer(t, repo, store)

	resp := getPlaybackStream(t, srv, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "playback-video-bytes" {
		t.Fatalf("body = %q", body)
	}
	if repo.touches != 1 {
		t.Fatalf("touches = %d, want 1", repo.touches)
	}
	if len(store.opened) != 1 || store.opened[0] != "videos/vod-42-playback.mp4" {
		t.Fatalf("opened paths = %#v", store.opened)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("content-type = %q, want video/mp4", ct)
	}
}

// TestStreamPlayback_StatsArtifactOnce guards against duplicate S3 HeadObject
// requests when serving an already-checked artifact.
func TestStreamPlayback_StatsArtifactOnce(t *testing.T) {
	artifactName := "vod-42-playback.mp4"
	artifactMime := "video/mp4"
	store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-playback.mp4": []byte("playback-video-bytes")}}
	repo := &signedRepo{
		video: doneVideo(),
		asset: &repository.VideoPlaybackAsset{
			VideoID:  42,
			Status:   repository.PlaybackAssetStatusReady,
			Filename: &artifactName,
			MimeType: &artifactMime,
		},
	}
	srv := playbackTestServer(t, repo, store)

	resp := getPlaybackStream(t, srv, 42)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if store.stats != 1 {
		t.Fatalf("Stat calls = %d, want 1 (no redundant HeadObject per request)", store.stats)
	}
}

// TestStreamPlayback_DoesNotTouchOnHeadOrRange keeps playback probes and
// continuation chunks from contending with SQLite recording writes.
func TestStreamPlayback_DoesNotTouchOnHeadOrRange(t *testing.T) {
	artifactName := "vod-42-playback.mp4"
	artifactMime := "video/mp4"
	store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-playback.mp4": []byte("playback-video-bytes")}}
	repo := &signedRepo{
		video: doneVideo(),
		asset: &repository.VideoPlaybackAsset{
			VideoID:  42,
			Status:   repository.PlaybackAssetStatusReady,
			Filename: &artifactName,
			MimeType: &artifactMime,
		},
	}
	srv := playbackTestServer(t, repo, store)

	headResp, err := http.Head(srv.URL + "/api/v1/videos/42/playback/stream")
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	headResp.Body.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/videos/42/playback/stream", nil)
	req.Header.Set("Range", "bytes=2-5")
	rangeResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Range GET: %v", err)
	}
	rangeResp.Body.Close()

	if repo.touches != 0 {
		t.Fatalf("touches = %d, want 0 for HEAD + mid-file Range requests", repo.touches)
	}
}

// TestStreamPlayback_TouchesOnSessionStartRange covers browsers opening with
// Range: bytes=0-; otherwise a frequently watched artifact could look unused.
func TestStreamPlayback_TouchesOnSessionStartRange(t *testing.T) {
	artifactName := "vod-42-playback.mp4"
	artifactMime := "video/mp4"
	store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-playback.mp4": []byte("playback-video-bytes")}}
	repo := &signedRepo{
		video: doneVideo(),
		asset: &repository.VideoPlaybackAsset{
			VideoID:  42,
			Status:   repository.PlaybackAssetStatusReady,
			Filename: &artifactName,
			MimeType: &artifactMime,
		},
	}
	srv := playbackTestServer(t, repo, store)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/videos/42/playback/stream", nil)
	req.Header.Set("Range", "bytes=0-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Range GET: %v", err)
	}
	resp.Body.Close()

	if repo.touches != 1 {
		t.Fatalf("touches = %d, want 1 for a bytes=0- session-start request", repo.touches)
	}
}

// TestStreamPlayback_DemotesStaleReadyRow lets clients fall back to stored
// parts after a ready artifact disappears.
func TestStreamPlayback_DemotesStaleReadyRow(t *testing.T) {
	artifactName := "vod-42-playback.mp4"
	artifactMime := "video/mp4"
	store := &signedStorage{bodies: map[string][]byte{}} // artifact absent
	repo := &signedRepo{
		video: doneVideo(),
		asset: &repository.VideoPlaybackAsset{
			VideoID:  42,
			Status:   repository.PlaybackAssetStatusReady,
			Filename: &artifactName,
			MimeType: &artifactMime,
		},
	}
	srv := playbackTestServer(t, repo, store)

	resp, err := http.Get(srv.URL + "/api/v1/videos/42/playback/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if repo.deletes != 1 {
		t.Fatalf("deletes = %d, want 1 (stale ready row demoted)", repo.deletes)
	}
	if repo.touches != 0 {
		t.Fatalf("touches = %d, want 0 (missing file must not be promoted)", repo.touches)
	}
}

func TestStreamPlayback_NonReadyAssetReturnsNotFound(t *testing.T) {
	repo := &signedRepo{
		video: doneVideo(),
		asset: &repository.VideoPlaybackAsset{
			VideoID: 42,
			Status:  repository.PlaybackAssetStatusBuilding,
		},
	}
	store := &signedStorage{bodies: map[string][]byte{
		"videos/vod-42-playback.mp4": []byte("playback-video-bytes"),
	}}
	srv := playbackTestServer(t, repo, store)

	resp := getPlaybackStream(t, srv, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if store.stats != 0 {
		t.Fatalf("Stat calls = %d, want 0 when asset is not ready", store.stats)
	}
	if repo.touches != 0 || repo.deletes != 0 {
		t.Fatalf("touches/deletes = %d/%d, want 0/0 for non-ready asset", repo.touches, repo.deletes)
	}
}

func TestStreamPlayback_TransientStatErrorServesWithoutDemoting(t *testing.T) {
	artifactName := "vod-42-playback.mp4"
	artifactMime := "video/mp4"
	store := &signedStorage{
		bodies: map[string][]byte{
			"videos/vod-42-playback.mp4": []byte("playback-video-bytes"),
		},
		statErrs: map[string][]error{
			"videos/vod-42-playback.mp4": {errors.New("transient HeadObject failure")},
		},
	}
	repo := &signedRepo{
		video: doneVideo(),
		asset: &repository.VideoPlaybackAsset{
			VideoID:  42,
			Status:   repository.PlaybackAssetStatusReady,
			Filename: &artifactName,
			MimeType: &artifactMime,
		},
	}
	srv := playbackTestServer(t, repo, store)

	resp := getPlaybackStream(t, srv, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "playback-video-bytes" {
		t.Fatalf("body = %q", body)
	}
	if repo.deletes != 0 {
		t.Fatalf("deletes = %d, want 0 on transient Stat error", repo.deletes)
	}
	if store.stats != 2 {
		t.Fatalf("Stat calls = %d, want initial failed Stat plus serve retry", store.stats)
	}
}

func TestStreamVideo_CompatibleMultipartWithoutCacheKeepsPartOneFallback(t *testing.T) {
	fps := 60.0
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
		},
	}
	srv := sessionPartTestServer(t, repo)

	resp, err := http.Get(srv.URL + "/api/v1/videos/42/parts/1/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "video-bytes" {
		t.Fatalf("body = %q", body)
	}
}

func TestStreamVideo_HEADCompatibleMultipartDoesNotBuildContinuousCache(t *testing.T) {
	fps := 60.0
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
		},
	}
	srv := sessionPartTestServer(t, repo)

	resp := headSessionStream(t, srv, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Fatalf("HEAD body length = %d, want 0", len(body))
	}
}

func TestStreamVideo_IncompatibleMultipartKeepsPartOneFallback(t *testing.T) {
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", Codec: repository.CodecH264},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "720", Codec: repository.CodecH264},
		},
	}
	srv := sessionPartTestServer(t, repo)

	resp, err := http.Get(srv.URL + "/api/v1/videos/42/parts/1/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "video-bytes" {
		t.Fatalf("body = %q", body)
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

// TestStreamSignedPart_headProbeReturnsSizeAndFilename covers consumers that
// probe transfer size and output filename before starting a large download.
func TestStreamSignedPart_headProbeReturnsSizeAndFilename(t *testing.T) {
	body := []byte("part02-bytes-of-a-known-length")
	store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-02.mp4": body}}
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4"},
			{PartIndex: 2, Filename: "vod-42-02.mp4"},
		},
	}
	srv := signedRouteTestServer(t, repo, store)
	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)

	resp := headSigned(t, srv, signer, 42, 2)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := resp.ContentLength, int64(len(body)); got != want {
		t.Fatalf("content-length = %d, want %d", got, want)
	}
	got, _ := io.ReadAll(resp.Body)
	if len(got) != 0 {
		t.Fatalf("HEAD body length = %d, want 0", len(got))
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="vod-42-02.mp4"` {
		t.Fatalf("content-disposition = %q, want the part filename as an attachment", cd)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("content-type = %q, want video/mp4", ct)
	}
	if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("accept-ranges = %q, want bytes", ar)
	}
}

// TestStreamSignedPart_headMatchesGetAcrossOutcomes ensures HEAD reveals
// no media that GET would refuse.
func TestStreamSignedPart_headMatchesGetAcrossOutcomes(t *testing.T) {
	valid := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)
	wrongKey := videodownload.NewSigner("not-the-secret", "https://app.example", time.Hour)

	deleted := doneVideo()
	when := time.Unix(2000, 0)
	deleted.DeletedAt = &when

	stillRecording := doneVideo()
	stillRecording.Status = repository.VideoStatusRunning

	onePart := []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}}
	served := func() storage.Storage { return &signedStorage{body: []byte("video-bytes")} }

	tests := []struct {
		name   string
		repo   *signedRepo
		store  storage.Storage
		signer *videodownload.Signer
		part   int32
		want   int
	}{
		{"served", &signedRepo{video: doneVideo(), parts: onePart}, served(), valid, 1, http.StatusOK},
		{"wrong signing key", &signedRepo{video: doneVideo(), parts: onePart}, served(), wrongKey, 1, http.StatusForbidden},
		{"deleted recording", &signedRepo{video: deleted, parts: onePart}, served(), valid, 1, http.StatusGone},
		{"unfinished recording", &signedRepo{video: stillRecording, parts: onePart}, served(), valid, 1, http.StatusNotFound},
		{"unknown part index", &signedRepo{video: doneVideo(), parts: onePart}, served(), valid, 9, http.StatusNotFound},
		{"media gone from storage", &signedRepo{video: doneVideo(), parts: onePart}, &signedStorage{bodies: map[string][]byte{}}, valid, 1, http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := signedRouteTestServer(t, tc.repo, tc.store)

			get := doSigned(t, srv, tc.signer, http.MethodGet, 42, tc.part)
			defer get.Body.Close()
			head := doSigned(t, srv, tc.signer, http.MethodHead, 42, tc.part)
			defer head.Body.Close()

			if get.StatusCode != tc.want {
				t.Fatalf("GET status = %d, want %d", get.StatusCode, tc.want)
			}
			if head.StatusCode != get.StatusCode {
				t.Fatalf("HEAD status = %d, GET status = %d; the methods must agree", head.StatusCode, get.StatusCode)
			}
			body, _ := io.ReadAll(head.Body)
			if len(body) != 0 {
				t.Fatalf("HEAD body length = %d, want 0", len(body))
			}
		})
	}
}

// TestStreamSignedPart_rangeRequestIsResumable checks that Content-Range
// reports the full size for consumers resuming interrupted downloads.
func TestStreamSignedPart_rangeRequestIsResumable(t *testing.T) {
	body := []byte("0123456789abcdef")
	store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-01.mp4": body}}
	repo := &signedRepo{video: doneVideo(), parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}}}
	srv := signedRouteTestServer(t, repo, store)
	signer := videodownload.NewSigner(signTestSecret, "https://app.example", time.Hour)

	signed, err := url.Parse(signer.PartURL(42, 1))
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+signed.Path+"?"+signed.RawQuery, nil)
	if err != nil {
		t.Fatalf("build GET: %v", err)
	}
	req.Header.Set("Range", "bytes=10-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "bytes 10-15/16" {
		t.Fatalf("content-range = %q, want bytes 10-15/16", cr)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "abcdef" {
		t.Fatalf("body = %q, want the requested tail", got)
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
	if _, _, ok := partPath(v, nil, 0); ok {
		t.Fatal("missing part must not invent a media path")
	}

}

// TestStreamPlayback_UnattachedStorageKeepsReadyRow treats absence on
// unattached storage as a retryable outage, preserving the artifact reference.
func TestStreamPlayback_UnattachedStorageKeepsReadyRow(t *testing.T) {
	artifactName := "vod-42-playback.mp4"
	artifactMime := "video/mp4"
	store := &signedStorage{bodies: map[string][]byte{}}
	repo := &signedRepo{
		video: doneVideo(),
		asset: &repository.VideoPlaybackAsset{
			VideoID:  42,
			Status:   repository.PlaybackAssetStatusReady,
			Filename: &artifactName,
			MimeType: &artifactMime,
		},
	}
	gate := gateFunc(func() error { return fmt.Errorf("%w: marker missing", storage.ErrUnattached) })
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithStorageGate(gate))

	resp, err := http.Get(srv.URL + "/api/v1/videos/42/playback/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if repo.deletes != 0 {
		t.Fatalf("deletes = %d, want 0 (ready row must survive an unattached volume)", repo.deletes)
	}
}
