//go:build ffmpeg

// Reusable test harness for downloader integration tests that need
// a real Service.run pipeline against a fake Twitch edge. Build tag
// `ffmpeg` because every consumer ends up shelling out to ffmpeg +
// ffprobe to remux the synthetic segments.

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
// missing. Mirrors the helper in remux/ffmpeg_real_test.go —
// duplicated because Go test files can't share symbols across
// packages.
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

// twitchEdge mocks Twitch's GQL + usher + media-playlist endpoints.
// Variant A is MPEG-TS at "480"; variant B is fMP4 at "360". Both
// H.264. Same codec keeps the SelectVariant codec filter out of
// the way; differing quality+format triggers the cross-codec split.
type twitchEdge struct {
	t      *testing.T
	server *httptest.Server
	opts   twitchEdgeOpts

	tsSegments [][]byte
	fmp4Init   []byte
	fmp4Segs   [][]byte

	mu          sync.Mutex
	aCursor     int
	aDropped    bool
	pendingJump int // consumed by next handlePlaylistA after NoteRestart
}

type twitchEdgeOpts struct {
	tsCount   int
	fmp4Count int
	windowA   int
	windowB   int
	baseSeqA  int
	baseSeqB  int

	// dropAfterServed: once aCursor reaches this, master flips to
	// B-only and variant A's media playlist 404s.
	dropAfterServed int

	// aEndlist: when aCursor reaches this, variant A's playlist
	// serves EXT-X-ENDLIST. 0 = never (live indefinitely).
	aEndlist int

	// postRestartSeqJump: after NoteRestart, the next playlist A
	// poll bumps aCursor by this amount, simulating a CDN window
	// rolled past the prior frontier during process downtime.
	postRestartSeqJump int
}

// defaultEdgeOpts: baseSeqB < baseSeqA + tsCount catches a re-anchor
// regression — Twitch doesn't share MEDIA-SEQUENCE across variants,
// so a re-anchor that carries the prior part's last seq forward
// would silently filter the new variant's segments.
func defaultEdgeOpts() twitchEdgeOpts {
	return twitchEdgeOpts{
		tsCount:         5,
		fmp4Count:       4,
		windowA:         3,
		windowB:         4,
		baseSeqA:        100,
		baseSeqB:        50,
		dropAfterServed: 3,
	}
}

func newTwitchEdge(t *testing.T, opts twitchEdgeOpts) *twitchEdge {
	t.Helper()
	e := &twitchEdge{t: t, opts: opts}

	tsDir := filepath.Join(t.TempDir(), "ts")
	fmp4Dir := filepath.Join(t.TempDir(), "fmp4")
	if err := os.MkdirAll(tsDir, 0o755); err != nil {
		t.Fatalf("mkdir tsDir: %v", err)
	}
	if err := os.MkdirAll(fmp4Dir, 0o755); err != nil {
		t.Fatalf("mkdir fmp4Dir: %v", err)
	}
	e.tsSegments = generateTSSegments(t, tsDir, opts.tsCount)
	if opts.fmp4Count > 0 {
		e.fmp4Init, e.fmp4Segs = generateFMP4Segments(t, fmp4Dir, opts.fmp4Count)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/gql", e.handleGQL)
	mux.HandleFunc("/api/channel/hls/", e.handleUsher)
	mux.HandleFunc("/playlist/A.m3u8", e.handlePlaylistA)
	mux.HandleFunc("/playlist/B.m3u8", e.handlePlaylistB)
	mux.HandleFunc("/seg/A/", e.handleSegA)
	mux.HandleFunc("/seg/B/init.mp4", e.handleInitB)
	mux.HandleFunc("/seg/B/", e.handleSegB)

	e.server = httptest.NewServer(mux)
	t.Cleanup(e.server.Close)
	return e
}

func (e *twitchEdge) URL() string { return e.server.URL }

// NoteRestart arms the postRestartSeqJump knob: the next
// handlePlaylistA poll will jump aCursor forward by
// opts.postRestartSeqJump before serving, simulating a CDN window
// that rolled while the harness service was down.
//
// Only meaningful when opts.postRestartSeqJump > 0. No-op
// otherwise so tests that don't care can call it unconditionally
// after spawning their post-restart Service.
func (e *twitchEdge) NoteRestart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.opts.postRestartSeqJump > 0 {
		e.pendingJump = e.opts.postRestartSeqJump
	}
}

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

// handleUsher serves the master playlist. After the variant-A drop
// fires the master lists only B; before that, both. fmp4Count==0
// in opts omits B entirely (single-variant scenarios).
func (e *twitchEdge) handleUsher(w http.ResponseWriter, _ *http.Request) {
	e.mu.Lock()
	dropped := e.aDropped
	e.mu.Unlock()

	base := e.server.URL
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	// audio_only mirrors Twitch's allow_audio_only=true response
	// shape; SelectVariant strips it but a missing MEDIA group
	// would change what we present to the selector.
	b.WriteString(`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aac",NAME="audio_only",URI="` + base + `/playlist/A.m3u8"` + "\n")
	if !dropped {
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=600000,RESOLUTION=160x480,CODECS=\"avc1.4d401f,mp4a.40.2\",FRAME-RATE=10.000,VIDEO=\"480p\"\n")
		fmt.Fprintf(&b, "%s/playlist/A.m3u8\n", base)
	}
	if e.opts.fmp4Count > 0 {
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=300000,RESOLUTION=160x360,CODECS=\"avc1.4d401e,mp4a.40.2\",FRAME-RATE=10.000,VIDEO=\"360p\"\n")
		fmt.Fprintf(&b, "%s/playlist/B.m3u8\n", base)
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, _ = io.WriteString(w, b.String())
}

func (e *twitchEdge) handlePlaylistA(w http.ResponseWriter, r *http.Request) {
	opts := e.opts
	e.mu.Lock()
	if e.aDropped {
		e.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if e.pendingJump > 0 {
		e.aCursor += e.pendingJump
		e.pendingJump = 0
	}
	if e.aCursor < opts.tsCount {
		e.aCursor++
	}
	cursor := e.aCursor
	if opts.dropAfterServed > 0 && cursor >= opts.dropAfterServed {
		e.aDropped = true
	}
	e.mu.Unlock()

	// Cap end at tsCount so a postRestartSeqJump doesn't push us
	// into nonexistent fixture indices.
	end := min(cursor, opts.tsCount)
	start := end - opts.windowA
	if start < 0 {
		start = 0
	}
	endlist := opts.aEndlist > 0 && cursor >= opts.aEndlist

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("#EXT-X-TARGETDURATION:1\n")
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", opts.baseSeqA+start)
	for i := start; i < end; i++ {
		b.WriteString("#EXTINF:1.000,\n")
		fmt.Fprintf(&b, "%s/seg/A/%d.ts\n", e.server.URL, i)
	}
	if endlist {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, _ = io.WriteString(w, b.String())
}

func (e *twitchEdge) handlePlaylistB(w http.ResponseWriter, _ *http.Request) {
	opts := e.opts
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

type harnessService struct {
	svc        *Service
	repo       repository.Repository
	storage    storage.Storage
	storageDir string
	scratchDir string
}

type harnessOpts struct {
	maxRestartGapSeconds int

	// inherited* fields simulate a process restart over the same
	// state. resumeOver bundles them so callers can't forget one.
	inheritedRepo       repository.Repository
	inheritedStorage    storage.Storage
	inheritedStorageDir string
	inheritedScratch    string
}

func newHarnessService(t *testing.T, edgeURL string) *harnessService {
	return newHarnessServiceWithOpts(t, edgeURL, harnessOpts{})
}

// resumeOver constructs a new Service over the prior's repo +
// storage + scratch — the production "same disks, fresh process"
// boot. Caller re-passes per-test config knobs via opts.
func resumeOver(t *testing.T, prior *harnessService, edgeURL string, opts ...func(*harnessOpts)) *harnessService {
	t.Helper()
	o := harnessOpts{
		inheritedRepo:       prior.repo,
		inheritedStorage:    prior.storage,
		inheritedStorageDir: prior.storageDir,
		inheritedScratch:    prior.scratchDir,
	}
	for _, fn := range opts {
		fn(&o)
	}
	return newHarnessServiceWithOpts(t, edgeURL, o)
}

func withMaxRestartGapSeconds(seconds int) func(*harnessOpts) {
	return func(o *harnessOpts) {
		o.maxRestartGapSeconds = seconds
	}
}

func newHarnessServiceWithOpts(t *testing.T, edgeURL string, opts harnessOpts) *harnessService {
	t.Helper()

	var repo repository.Repository
	if opts.inheritedRepo != nil {
		repo = opts.inheritedRepo
	} else {
		db := testdb.NewSQLiteDB(t)
		repo = sqliteadapter.New(db)
	}

	var store storage.Storage
	var storageDir string
	if opts.inheritedStorage != nil {
		store = opts.inheritedStorage
		storageDir = opts.inheritedStorageDir
	} else {
		storageDir = t.TempDir()
		s, err := storage.NewLocal(storageDir)
		if err != nil {
			t.Fatalf("storage: %v", err)
		}
		store = s
	}

	scratchDir := opts.inheritedScratch
	if scratchDir == "" {
		scratchDir = t.TempDir()
	}

	maxRestartGap := opts.maxRestartGapSeconds
	if maxRestartGap == 0 {
		maxRestartGap = 120
	}

	cfg := &config.Config{
		Env: config.Environment{
			ScratchDir: scratchDir,
		},
		App: config.AppConfig{
			Download: config.DownloadConfig{
				MaxConcurrent:        2,
				SegmentConcurrency:   2,
				NetworkAttempts:      2,
				ServerErrorAttempts:  2,
				CDNLagAttempts:       2,
				AuthRefreshAttempts:  2,
				MaxGapRatio:          0.5,
				MaxRestartGapSeconds: maxRestartGap,
			},
			TitleTracking: config.TitleTrackingConfig{
				Mode: config.TitleTrackingModeOff,
			},
		},
	}

	logSink := io.Discard
	if testing.Verbose() {
		logSink = os.Stderr
	}
	log := slog.New(slog.NewTextHandler(logSink, nil))
	svc := NewService(cfg, repo, store, nil, nil, nil, log)

	// In-package field access avoids adding a test-only constructor
	// or production URL config knobs.
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
		storage:    store,
		storageDir: storageDir,
		scratchDir: scratchDir,
	}
}

func drainProgress(ch <-chan Progress) []Progress {
	var out []Progress
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// waitForVideoStatus polls because the progress channel closes
// BEFORE MarkVideoDone (close is deferred; MarkVideoDone runs in
// the function body). dbCtx writes survive ctx cancel.
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

func waitForJobResumeState(t *testing.T, repo repository.Repository, jobID string, pred func(*ResumeState) bool, timeout time.Duration) *ResumeState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		job, err := repo.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("get job %s: %v", jobID, err)
		}
		state, err := UnmarshalResumeState(job.ResumeState)
		if err != nil {
			t.Fatalf("unmarshal resume state: %v", err)
		}
		if pred(state) {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s resume_state never satisfied predicate within %s (last: %+v)", jobID, timeout, state)
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

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
