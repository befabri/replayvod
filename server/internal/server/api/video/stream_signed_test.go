package video

import (
	"bytes"
	"context"
	"errors"
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

// fakeBuilder records lazy playback-artifact build triggers.
type fakeBuilder struct {
	mu    sync.Mutex
	calls []int64
}

func (f *fakeBuilder) StartBuild(_ context.Context, videoID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, videoID)
}

func (f *fakeBuilder) videoIDs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.calls...)
}

const signTestSecret = "stream-signing-secret"

// signedRepo implements the repository methods the stream handlers use;
// embedding the interface gives the rest nil bodies (never called here).
type signedRepo struct {
	repository.Repository
	video        *repository.Video
	videos       map[int64]*repository.Video
	videoErr     error
	parts        []repository.VideoPart
	partsByVideo map[int64][]repository.VideoPart
	asset        *repository.VideoPlaybackAsset
	assets       map[int64]*repository.VideoPlaybackAsset
	touches      int
	deletes      int
}

func (r *signedRepo) GetVideo(_ context.Context, id int64) (*repository.Video, error) {
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

// signedStorage serves canned bytes for any path.
type signedStorage struct {
	storage.Storage
	body     []byte
	bodies   map[string][]byte
	statErrs map[string][]error
	opened   []string
	stats    int // Stat call count (each is a HeadObject on S3)
}

type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

func (s *signedStorage) Open(_ context.Context, path string) (io.ReadSeekCloser, error) {
	s.opened = append(s.opened, path)
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

// signedTestServer wires the signed route on a chi router backed by the fakes.
func signedTestServer(t *testing.T, repo repository.Repository) *httptest.Server {
	t.Helper()
	return signedRouteTestServer(t, repo, &signedStorage{body: []byte("video-bytes")})
}

func signedRouteTestServer(t *testing.T, repo repository.Repository, store storage.Storage) *httptest.Server {
	t.Helper()
	h := NewStreamHandler(repo, store, videodownload.NewVerifier(signTestSecret), testClientLogger())
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
	h := NewStreamHandler(repo, store, videodownload.NewVerifier(signTestSecret), log, opts...)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h.SetupRoutes(r, func(next http.Handler) http.Handler { return next })
	})
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
	req, err := http.NewRequest(http.MethodHead, srv.URL+"/api/v1/videos/"+strconv.FormatInt(videoID, 10)+"/stream", nil)
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

	// Part 2 (not just the first part) must be reachable — the whole point of per-part URLs.
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

// Playing a recording must lazily kick its single-file artifact build — once
// per video, no matter how many part/range requests a single view fires (a
// player issues a HEAD probe plus many range GETs across parts).
func TestStreamPart_firstPlayKicksLazyBuildOncePerVideo(t *testing.T) {
	builder := &fakeBuilder{}
	repo := &signedRepo{
		video: doneVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4"},
			{PartIndex: 2, Filename: "vod-42-02.mp4"},
		},
	}
	srv := sessionPartTestServer(t, repo, WithPlaybackBuilder(builder))

	for _, part := range []int32{1, 1, 2, 1} {
		resp := getSessionPart(t, srv, 42, part)
		resp.Body.Close()
	}

	if got := builder.videoIDs(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("StartBuild calls = %#v, want exactly [42]", got)
	}
}

// The signed per-part download route (recording-webhook consumers) must NOT
// trigger a build — only a real dashboard view does.
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
	h := NewStreamHandler(repo, store, videodownload.NewVerifier(signTestSecret), testClientLogger(), WithPlaybackBuilder(builder))
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

// A client that aborts a part request mid-flight (a video player canceling its
// overlapping Range GETs on every seek or part switch) cancels the request
// context, which surfaces from the repo as context.Canceled. That's the
// client's doing, not a server fault: the route must answer 499 and stay
// silent, not log an ERROR and hand back a misleading 500.
func TestStreamPart_clientCanceledIsNotServerError(t *testing.T) {
	logs := &capturingHandler{}
	repo := &signedRepo{
		videoErr: context.Canceled,
		parts:    []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}},
	}
	srv := streamRouteTestServer(t, repo, &signedStorage{body: []byte("video-bytes")}, slog.New(logs))

	resp := getSessionPart(t, srv, 42, 1)
	defer resp.Body.Close()

	if resp.StatusCode != statusClientClosed {
		t.Fatalf("status = %d, want %d (client closed request)", resp.StatusCode, statusClientClosed)
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

// A playback request must Stat the artifact exactly once. streamPlayback Stats
// for its stale-row check and reuses that FileInfo; a second Stat in
// serveStorageFile would be a wasted HeadObject on S3 (a hot path: HEAD probe +
// many Range GETs per view).
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

// HTML5 players fire a HEAD probe and many Range GETs per view; only the
// initial full GET should bump the LRU clock, or playback becomes a write storm
// (and on SQLite serializes against live recording writes).
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

// Browsers open a <video> with `Range: bytes=0-`, so the session-start range
// must bump the LRU clock or a hot artifact's last_accessed_at would freeze at
// build time and Prune would evict the most-watched videos first.
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

// A ready row whose artifact file is gone (e.g. a retention pass purged the
// object but its row delete then failed) must self-heal: drop the row so
// video.getById stops advertising the artifact and the watch page falls back to
// /parts, rather than 404ing forever. The missing file must not be touched.
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

// The legacy /stream fallback for a multi-part recording must warn ONCE per
// video, not on every request — a player issues a HEAD probe plus many range
// GETs, and per-request logging floods the logs for a single view.
func TestStreamVideo_MultipartFallbackWarnsOncePerVideo(t *testing.T) {
	fps := 60.0
	video42Parts := []repository.VideoPart{
		{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
		{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
	}
	video43Parts := []repository.VideoPart{
		{PartIndex: 1, Filename: "vod-43-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
		{PartIndex: 2, Filename: "vod-43-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264},
	}
	repo := &signedRepo{
		videos: map[int64]*repository.Video{
			42: doneVideo(42),
			43: doneVideo(43),
		},
		partsByVideo: map[int64][]repository.VideoPart{
			42: video42Parts,
			43: video43Parts,
		},
	}
	logs := &capturingHandler{}
	srv := streamRouteTestServer(t, repo, &signedStorage{body: []byte("part01-bytes")}, slog.New(logs))

	requestLegacyStreamView(t, srv, 42)
	requestLegacyStreamView(t, srv, 43)
	requestLegacyStreamView(t, srv, 42)

	if n := logs.countWarn("only part 01"); n != 2 {
		t.Fatalf("multipart fallback warnings = %d across two videos and a repeat view, want 2", n)
	}
}

func requestLegacyStreamView(t *testing.T, srv *httptest.Server, videoID int64) {
	t.Helper()
	// One view = a HEAD probe plus a couple of range GETs.
	base := srv.URL + "/api/v1/videos/" + strconv.FormatInt(videoID, 10) + "/stream"
	headResp, err := http.Head(base)
	if err != nil {
		t.Fatalf("HEAD video %d: %v", videoID, err)
	}
	headResp.Body.Close()
	for _, rng := range []string{"bytes=0-", "bytes=4-"} {
		req, _ := http.NewRequest(http.MethodGet, base, nil)
		req.Header.Set("Range", rng)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET video %d %s: %v", videoID, rng, err)
		}
		resp.Body.Close()
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

	resp, err := http.Get(srv.URL + "/api/v1/videos/42/stream")
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

	resp, err := http.Get(srv.URL + "/api/v1/videos/42/stream")
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
