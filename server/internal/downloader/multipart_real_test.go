//go:build ffmpeg

// Real-pipeline tests for the multi-part part-split path (Phase 6f).
// Excluded from default `go test ./...` because they shell out to
// ffmpeg + ffprobe and stand up a fake Twitch edge end-to-end. Enable
// with:
//
//	go test -tags ffmpeg -count=1 ./internal/downloader/... -run Multipart
//
// This file is the project's first downloader-level integration
// harness — the smaller hls/orchestrator_test.go fixtures only cover
// the HLS layer in isolation. The harness is deliberately built
// reusable: subsequent tests that want to exercise auth-refresh
// cascades, stitched-ad behavior at runtime, or future Phase 6h/6i
// features can stand up a twitchEdge with different knobs and run
// the same Service.run pipeline against it.
//
// Cost per test: ~5-10s wall clock (most of it ffmpeg encoding the
// fixture segments and HLS poll-tick wait). ffmpeg + ffprobe must
// be on PATH; the harness Skips otherwise.

package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// requireFFmpegHarness skips the test when ffmpeg / ffprobe are
// missing. Mirrors the helper in remux/ffmpeg_real_test.go — duplicated
// because Go test files can't share symbols across packages.
func requireFFmpegHarness(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}
}

// generateTSSegments writes count single-second MPEG-TS fragments to
// dir, named seg00.ts ... segNN.ts, returning their bytes in order.
//
// One ffmpeg invocation produces all fragments via the HLS muxer's
// segment splitter — much faster than count separate ffmpeg calls.
// We discard the playlist file ffmpeg writes; the harness builds its
// own m3u8 with the right MEDIA-SEQUENCE / TARGETDURATION + sliding
// window for the live-stream simulation.
func generateTSSegments(t *testing.T, dir string, count int) [][]byte {
	t.Helper()
	durStr := strconv.Itoa(count) // count seconds total, 1s per segment
	// -g 10 + force_key_frames every second is load-bearing: without
	// per-second IDR frames the HLS muxer keeps the full input in a
	// single GOP and emits one big segment instead of `count`. The
	// keyint params drive the segmenter, not the encoded quality.
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration="+durStr,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-g", "10", "-keyint_min", "10",
		"-force_key_frames", "expr:gte(t,n_forced)",
		"-c:a", "aac",
		"-f", "hls",
		"-hls_time", "1",
		"-hls_segment_type", "mpegts",
		"-hls_playlist_type", "vod",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(dir, "seg%02d.ts"),
		filepath.Join(dir, "playlist.m3u8"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate TS segments: %v\n%s", err, out)
	}
	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("seg%02d.ts", i)))
		if err != nil {
			t.Fatalf("read TS seg %d: %v", i, err)
		}
		out = append(out, data)
	}
	return out
}

// generateFMP4Segments writes init.mp4 + count .m4s fragments to dir.
// Returns the init bytes and the ordered fragment bytes. Fragment
// indices written by ffmpeg start at 0 (seg0.m4s, seg1.m4s, ...).
func generateFMP4Segments(t *testing.T, dir string, count int) (init []byte, segs [][]byte) {
	t.Helper()
	durStr := strconv.Itoa(count)
	// Same keyframe-per-second forcing as the TS path; otherwise the
	// muxer emits a single fmp4 fragment for the whole input.
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration="+durStr,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-g", "10", "-keyint_min", "10",
		"-force_key_frames", "expr:gte(t,n_forced)",
		"-c:a", "aac",
		"-f", "hls",
		"-hls_time", "1",
		"-hls_segment_type", "fmp4",
		"-hls_playlist_type", "vod",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(dir, "seg%d.m4s"),
		"-hls_fmp4_init_filename", "init.mp4",
		filepath.Join(dir, "playlist.m3u8"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fMP4 segments: %v\n%s", err, out)
	}
	init, err := os.ReadFile(filepath.Join(dir, "init.mp4"))
	if err != nil {
		t.Fatalf("read init.mp4: %v", err)
	}
	segs = make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("seg%d.m4s", i)))
		if err != nil {
			t.Fatalf("read fMP4 seg %d: %v", i, err)
		}
		segs = append(segs, data)
	}
	return init, segs
}

// twitchEdge stands up an httptest.Server that mocks Twitch's
// production GQL + usher + media-playlist endpoints with switchable
// state. Built for Phase 6f's part-split test (variant A drops mid-
// stream, master starts exposing only variant B) but designed for
// reuse — knobs are explicit fields, not test-specific globals.
//
// Variant A is MPEG-TS at quality "480"; variant B is fMP4 at quality
// "360". Both H.264. The (quality, codec) pair differs across the
// boundary so the spec's "split when Kind, Codec, or Quality differ"
// rule fires; using H.264 for both keeps the codec-allowed filter in
// SelectVariant from interfering.
type twitchEdge struct {
	t      *testing.T
	server *httptest.Server

	// Fixture bytes prepared at construction. Held in memory because
	// the test doesn't need on-disk persistence and serving from a
	// []byte avoids a per-segment file open.
	tsSegments [][]byte
	fmp4Init   []byte
	fmp4Segs   [][]byte

	// Live-window simulation for variant A: cursor advances by one
	// segment per playlist poll (capped at len(tsSegments)). The
	// playlist serves the last `windowA` segments. baseSeq lets us
	// pick a non-zero MEDIA-SEQUENCE base for realism.
	mu       sync.Mutex
	aCursor  int  // highest TS seq served so far (0..len-1)
	bCursor  int  // highest FMP4 seq served so far
	aDropped bool // true once the orchestrator has fetched enough A; flips master + 404s A's playlist
}

// twitchEdgeOpts carries knobs that future tests might want to tweak
// (segment counts, quality strings, etc.). Phase 6f only needs the
// defaults.
type twitchEdgeOpts struct {
	tsCount   int // number of TS fragments for variant A
	fmp4Count int // number of fMP4 fragments for variant B
	windowA   int // sliding window size on variant A's playlist
	windowB   int // sliding window size on variant B's playlist
	baseSeqA  int
	baseSeqB  int
	// dropAfterServed: once aCursor >= dropAfterServed the master
	// switches to "B only" and variant A's media playlist returns 404.
	dropAfterServed int
}

func defaultEdgeOpts() twitchEdgeOpts {
	// baseSeqB < baseSeqA's last is intentional: Twitch doesn't share
	// MEDIA-SEQUENCE counters across variants, so the new variant's
	// first seq can be lower than the old variant's last. The
	// part-boundary code MUST re-anchor the resume frontier from the
	// new variant's MEDIA-SEQUENCE base; if it instead reuses the
	// old variant's last seq + 1 as a "starting" filter, the poller
	// silently drops every segment of the new variant. This config
	// makes that regression a hard failure instead of a coincidental
	// pass — keep baseSeqB < baseSeqA + tsCount.
	return twitchEdgeOpts{
		tsCount:         5,
		fmp4Count:       4,
		windowA:         3,
		windowB:         4, // entire B as VOD with ENDLIST
		baseSeqA:        100,
		baseSeqB:        50, // < baseSeqA + tsCount → catches anchor regressions
		dropAfterServed: 3,  // serve 3 segments of A, then drop
	}
}

func newTwitchEdge(t *testing.T, opts twitchEdgeOpts) *twitchEdge {
	t.Helper()
	e := &twitchEdge{t: t}

	tsDir := filepath.Join(t.TempDir(), "ts")
	fmp4Dir := filepath.Join(t.TempDir(), "fmp4")
	if err := os.MkdirAll(tsDir, 0o755); err != nil {
		t.Fatalf("mkdir tsDir: %v", err)
	}
	if err := os.MkdirAll(fmp4Dir, 0o755); err != nil {
		t.Fatalf("mkdir fmp4Dir: %v", err)
	}
	e.tsSegments = generateTSSegments(t, tsDir, opts.tsCount)
	e.fmp4Init, e.fmp4Segs = generateFMP4Segments(t, fmp4Dir, opts.fmp4Count)

	mux := http.NewServeMux()
	mux.HandleFunc("/gql", e.handleGQL)
	mux.HandleFunc("/api/channel/hls/", e.handleUsher(opts))
	mux.HandleFunc("/playlist/A.m3u8", e.handlePlaylistA(opts))
	mux.HandleFunc("/playlist/B.m3u8", e.handlePlaylistB(opts))
	mux.HandleFunc("/seg/A/", e.handleSegA)
	mux.HandleFunc("/seg/B/init.mp4", e.handleInitB)
	mux.HandleFunc("/seg/B/", e.handleSegB)

	e.server = httptest.NewServer(mux)
	t.Cleanup(e.server.Close)
	return e
}

func (e *twitchEdge) URL() string { return e.server.URL }

// handleGQL responds to PlaybackAccessToken with a stub token.
// The real client will pass the value/signature through to usher
// where we ignore them — they only need to be non-empty.
func (e *twitchEdge) handleGQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]any{
		"data": map[string]any{
			"streamPlaybackAccessToken": map[string]string{
				"value":     "fake-token-value",
				"signature": "fake-signature",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleUsher serves the master playlist. State machine: before
// dropAfterServed segments of A have been served, the master lists
// both A and B; after, only B. The media-playlist URLs point back
// at this server's /playlist/A.m3u8 and /playlist/B.m3u8.
func (e *twitchEdge) handleUsher(opts twitchEdgeOpts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		dropped := e.aDropped
		e.mu.Unlock()

		base := e.server.URL
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		b.WriteString("#EXT-X-VERSION:3\n")
		// Audio-only entry to mirror Twitch's allow_audio_only=true
		// (parser strips it; but a missing MEDIA group changes the
		// shape we present to SelectVariant).
		b.WriteString(`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aac",NAME="audio_only",URI="` + base + `/playlist/A.m3u8"` + "\n")
		if !dropped {
			fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=600000,RESOLUTION=160x480,CODECS=\"avc1.4d401f,mp4a.40.2\",FRAME-RATE=10.000,VIDEO=\"480p\"\n")
			fmt.Fprintf(&b, "%s/playlist/A.m3u8\n", base)
		}
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=300000,RESOLUTION=160x360,CODECS=\"avc1.4d401e,mp4a.40.2\",FRAME-RATE=10.000,VIDEO=\"360p\"\n")
		fmt.Fprintf(&b, "%s/playlist/B.m3u8\n", base)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, b.String())
	}
}

// handlePlaylistA serves variant A's media playlist with a sliding
// window. Each poll advances the cursor by one segment until we
// reach dropAfterServed, at which point the next poll returns 404
// and the master flips to "B only".
//
// Live (no ENDLIST) until the drop fires — the orchestrator's
// poller keeps coming back for more segments; the test relies on
// the 404 to break out of the part.
func (e *twitchEdge) handlePlaylistA(opts twitchEdgeOpts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		if e.aDropped {
			e.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		// Advance cursor (lazy: serve what we have, then bump for the
		// next poll). aCursor is 1-indexed in the "served-count"
		// sense, so equality with dropAfterServed flips the switch.
		if e.aCursor < opts.tsCount {
			e.aCursor++
		}
		cursor := e.aCursor
		if cursor >= opts.dropAfterServed {
			e.aDropped = true
		}
		e.mu.Unlock()

		// Sliding window: last `windowA` available segments.
		start := cursor - opts.windowA
		if start < 0 {
			start = 0
		}
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		b.WriteString("#EXT-X-VERSION:3\n")
		b.WriteString("#EXT-X-TARGETDURATION:1\n")
		fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", opts.baseSeqA+start)
		for i := start; i < cursor; i++ {
			b.WriteString("#EXTINF:1.000,\n")
			fmt.Fprintf(&b, "%s/seg/A/%d.ts\n", e.server.URL, i)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, b.String())
	}
}

// handlePlaylistB serves variant B's media playlist as a complete
// VOD (ENDLIST present). The orchestrator polls once, fetches all
// fmp4 segments, and exits cleanly.
func (e *twitchEdge) handlePlaylistB(opts twitchEdgeOpts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		b.WriteString("#EXT-X-VERSION:6\n")
		b.WriteString("#EXT-X-TARGETDURATION:1\n")
		fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", opts.baseSeqB)
		fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"%s/seg/B/init.mp4\"\n", e.server.URL)
		for i := 0; i < opts.fmp4Count; i++ {
			b.WriteString("#EXTINF:1.000,\n")
			fmt.Fprintf(&b, "%s/seg/B/%d.m4s\n", e.server.URL, i)
		}
		b.WriteString("#EXT-X-ENDLIST\n")
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, b.String())
	}
}

func (e *twitchEdge) handleSegA(w http.ResponseWriter, r *http.Request) {
	idx, ok := segIndexFromPath(r.URL.Path, "/seg/A/", ".ts")
	if !ok || idx < 0 || idx >= len(e.tsSegments) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	_, _ = w.Write(e.tsSegments[idx])
}

func (e *twitchEdge) handleInitB(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "video/iso.segment")
	_, _ = w.Write(e.fmp4Init)
}

func (e *twitchEdge) handleSegB(w http.ResponseWriter, r *http.Request) {
	idx, ok := segIndexFromPath(r.URL.Path, "/seg/B/", ".m4s")
	if !ok || idx < 0 || idx >= len(e.fmp4Segs) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "video/iso.segment")
	_, _ = w.Write(e.fmp4Segs[idx])
}

func segIndexFromPath(path, prefix, ext string) (int, bool) {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, ext) {
		return 0, false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), ext)
	idx, err := strconv.Atoi(name)
	if err != nil {
		return 0, false
	}
	return idx, true
}

// newHarnessService builds a Service wired to a sqlite testdb, local
// storage under t.TempDir, and the supplied twitch edge URL. Callers
// hold the returned bits so they can assert on the repo / storage
// after Service.run completes.
type harnessService struct {
	svc        *Service
	repo       repository.Repository
	storageDir string
	scratchDir string
}

func newHarnessService(t *testing.T, edgeURL string) *harnessService {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)

	storageDir := t.TempDir()
	store, err := storage.NewLocal(storageDir)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	scratchDir := t.TempDir()
	cfg := &config.Config{
		Env: config.Environment{
			ScratchDir: scratchDir,
		},
		App: config.AppConfig{
			Download: config.DownloadConfig{
				MaxConcurrent:       2,
				SegmentConcurrency:  2,
				NetworkAttempts:     2,
				ServerErrorAttempts: 2,
				CDNLagAttempts:      2,
				AuthRefreshAttempts: 2,
				MaxGapRatio:         0.5, // permissive: synthetic streams
				EnableAV1:           false,
				DisableHEVC:         false,
			},
			TitleTracking: config.TitleTrackingConfig{
				Mode: config.TitleTrackingModeOff,
			},
		},
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(cfg, repo, store, nil, nil, nil, log)

	// Override the production Twitch endpoints with the test edge.
	// Field access is unexported; the test file lives in package
	// downloader so this is in-bounds. Avoids polluting production
	// config with test-only URL overrides.
	svc.twitch = twitch.New(twitch.Config{
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
		ClientID:     "harness",
		UserAgent:    "harness",
		DeviceID:     "harness-device",
		GQLURL:       edgeURL + "/gql",
		IntegrityURL: edgeURL + "/integrity",
		UsherBaseURL: edgeURL,
	}, log)

	return &harnessService{
		svc:        svc,
		repo:       repo,
		storageDir: storageDir,
		scratchDir: scratchDir,
	}
}

// drainProgress consumes all events on ch into a slice. Returns once
// the channel is closed (which run() does via the deferred
// close(d.progressCh)).
func drainProgress(ch <-chan Progress) []Progress {
	var out []Progress
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// waitForVideoStatus polls the videos row for the given id until it
// reaches the wanted status or the timeout fires. Returns the final
// row. The orchestrator's run goroutine writes status transitions on
// dbCtx so they survive ctx cancel — the test polls because the
// progress channel closes BEFORE the terminal MarkVideoDone in some
// edge cases (the close runs in a defer, MarkVideoDone runs earlier
// in the function body).
func waitForVideoStatus(t *testing.T, repo repository.Repository, id int64, want string, timeout time.Duration) *repository.Video {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		v, err := repo.GetVideo(context.Background(), id)
		if err != nil {
			t.Fatalf("get video %d: %v", id, err)
		}
		if v.Status == want {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("video %d status=%q after %s, want %q (error=%v)", id, v.Status, timeout, want, derefStr(v.Error))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// TestMultipart_VariantDropSplitsIntoTwoParts is the AC-CROSS-1
// acceptance test (.docs/spec/download-pipeline.md:880). The
// downloader records variant A (MPEG-TS, 480p) for the first three
// segments; the master then drops A and B (fMP4, 360p) takes over.
// We assert the orchestrator finalized two video_parts rows, that
// the videos row aggregates duration/size across them, that two
// distinct files landed on storage, and that the progress emitter
// transitioned PartIndex from 1 to 2.
func TestMultipart_VariantDropSplitsIntoTwoParts(t *testing.T) {
	requireFFmpegHarness(t)

	edge := newTwitchEdge(t, defaultEdgeOpts())
	h := newHarnessService(t, edge.URL())
	defer h.svc.Shutdown()

	// Seed a channel row so resume / future tests see consistent
	// state. Not strictly required by Start() but cheap and matches
	// production preconditions.
	if _, err := h.repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_login",
		BroadcasterName:  "Harness Login",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	jobID, err := h.svc.Start(context.Background(), Params{
		BroadcasterID:    "test-bid",
		BroadcasterLogin: "harness_login",
		DisplayName:      "Harness Login",
		Quality:          repository.QualityHigh, // → "1080" preferred; falls back to 480 (variant A)
		RecordingType:    twitch.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Drain progress in a goroutine so we capture every event without
	// ever blocking the bridge — the orchestrator non-blocking-sends,
	// but a slow drain still misses events that get superseded.
	progressCh := h.svc.Subscribe(jobID)
	if progressCh == nil {
		t.Fatal("Subscribe returned nil channel")
	}
	progressDone := make(chan struct{})
	var events []Progress
	go func() {
		events = drainProgress(progressCh)
		close(progressDone)
	}()

	// Resolve the video row from the job to get its ID.
	job, err := h.repo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	videoID := job.VideoID

	// Wait for terminal status. 60s is generous — the test runs
	// ~3-5s of HLS poll ticks (1s target duration × ~5 polls) plus
	// two ffmpeg remux + probe + thumbnail passes (~1-2s each).
	video := waitForVideoStatus(t, h.repo, videoID, repository.VideoStatusDone, 60*time.Second)

	// Drain the rest of the progress events.
	select {
	case <-progressDone:
	case <-time.After(5 * time.Second):
		t.Fatal("progress channel did not close 5s after video DONE")
	}

	// Assertion 1: two video_parts rows.
	parts, err := h.repo.ListVideoParts(context.Background(), videoID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("video_parts count = %d, want 2 (parts: %+v)", len(parts), parts)
	}

	// Assertion 2: parts differ in (quality, segment_format) and are
	// in the right order.
	if parts[0].PartIndex != 1 || parts[1].PartIndex != 2 {
		t.Errorf("part_index ordering = [%d, %d], want [1, 2]", parts[0].PartIndex, parts[1].PartIndex)
	}
	if parts[0].Quality != "480" {
		t.Errorf("part 1 quality = %q, want \"480\"", parts[0].Quality)
	}
	if parts[1].Quality != "360" {
		t.Errorf("part 2 quality = %q, want \"360\"", parts[1].Quality)
	}
	if parts[0].SegmentFormat != "ts" {
		t.Errorf("part 1 segment_format = %q, want \"ts\"", parts[0].SegmentFormat)
	}
	if parts[1].SegmentFormat != "fmp4" {
		t.Errorf("part 2 segment_format = %q, want \"fmp4\"", parts[1].SegmentFormat)
	}

	// Assertion 3: videos row aggregates duration + size from the
	// two parts. Float comparison uses a small epsilon because
	// probe.Duration is a sub-second-precision float.
	if video.DurationSeconds == nil || video.SizeBytes == nil {
		t.Fatalf("video duration/size unset: dur=%v size=%v", video.DurationSeconds, video.SizeBytes)
	}
	wantDur := parts[0].DurationSeconds + parts[1].DurationSeconds
	wantSize := parts[0].SizeBytes + parts[1].SizeBytes
	if abs(*video.DurationSeconds-wantDur) > 0.001 {
		t.Errorf("video.duration_seconds = %f, want sum of parts %f", *video.DurationSeconds, wantDur)
	}
	if *video.SizeBytes != wantSize {
		t.Errorf("video.size_bytes = %d, want sum of parts %d", *video.SizeBytes, wantSize)
	}

	// Assertion 4: two distinct files on storage at the per-part
	// paths the spec defines (videos/<base>-partNN.mp4).
	for _, p := range parts {
		path := filepath.Join(h.storageDir, "videos", p.Filename)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("storage file missing for part %d at %q: %v", p.PartIndex, path, err)
			continue
		}
		if info.Size() != p.SizeBytes {
			t.Errorf("part %d storage size = %d, video_parts.size_bytes = %d", p.PartIndex, info.Size(), p.SizeBytes)
		}
		// Sanity: filename carries the -partNN suffix.
		if !strings.Contains(p.Filename, fmt.Sprintf("-part%02d", p.PartIndex)) {
			t.Errorf("part %d filename %q missing -part%02d suffix", p.PartIndex, p.Filename, p.PartIndex)
		}
	}

	// Assertion 5: progress events crossed the part boundary. The
	// emitter's setPart fires at least one event with PartIndex=2.
	maxPart := 0
	for _, ev := range events {
		if ev.PartIndex > maxPart {
			maxPart = ev.PartIndex
		}
	}
	if maxPart < 2 {
		t.Errorf("max PartIndex in progress events = %d, want ≥ 2", maxPart)
	}

	// Assertion 6: stream resolver uses video_parts.filename. We
	// don't actually exercise the HTTP handler here (different
	// package), but we sanity-check that part 1's storage file is
	// what the resolver would pick first by our convention.
	if !strings.HasSuffix(parts[0].Filename, "-part01.mp4") {
		t.Errorf("part 1 filename = %q, want suffix \"-part01.mp4\"", parts[0].Filename)
	}
}

// abs returns the absolute value of f without pulling in math.
func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
