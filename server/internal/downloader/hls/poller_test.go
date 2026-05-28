package hls

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// playlistServer returns an httptest server that answers every
// request with status + body. status 0 is treated as 200.
func playlistServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runPollerCollect runs p.Run to completion, draining every job out
// of the output channel and reporting the (optional) first
// PollResult plus the terminal error. Relies on Run's `defer
// close(out)` firing on every return path so the range terminates.
// The output channel is generously buffered so Run never blocks on a
// send — these tests assert on the produced values, not on
// backpressure timing.
func runPollerCollect(ctx context.Context, p *Poller) (pr PollResult, gotFirst bool, jobs []segmentJob, err error) {
	first := make(chan PollResult, 1)
	out := make(chan segmentJob, 256)
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(ctx, first, out) }()
	for j := range out {
		jobs = append(jobs, j)
	}
	err = <-errCh
	select {
	case pr = <-first:
		gotFirst = true
	default:
	}
	return pr, gotFirst, jobs, err
}

// --- fetchAndParse: status-code and parse branches ---------------

func TestFetchAndParse_Happy_ResolvesRelativeURIs(t *testing.T) {
	const body = "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-TARGETDURATION:2\n" +
		"#EXT-X-MEDIA-SEQUENCE:10\n" +
		"#EXTINF:2.000,\nseg10.ts\n" +
		"#EXTINF:2.000,\nseg11.ts\n" +
		"#EXT-X-ENDLIST\n"
	srv := playlistServer(t, http.StatusOK, body)
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}

	pl, err := p.fetchAndParse(context.Background())
	if err != nil {
		t.Fatalf("fetchAndParse: %v", err)
	}
	if pl.Kind != SegmentKindTS {
		t.Errorf("Kind=%s, want ts", pl.Kind)
	}
	if pl.MediaSequenceBase != 10 {
		t.Errorf("MediaSequenceBase=%d, want 10", pl.MediaSequenceBase)
	}
	if !pl.EndList {
		t.Error("EndList=false, want true")
	}
	// Relative URIs must come back resolved against the playlist URL.
	want := srv.URL + "/seg10.ts"
	if pl.Segments[0].URI != want {
		t.Errorf("Segments[0].URI=%q, want %q", pl.Segments[0].URI, want)
	}
}

func TestFetchAndParse_AuthRetryable(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := playlistServer(t, status, "denied")
		p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}
		_, err := p.fetchAndParse(context.Background())
		if !errors.Is(err, ErrPlaylistAuth) {
			t.Errorf("status %d: err=%v, want ErrPlaylistAuth", status, err)
		}
		if errors.Is(err, ErrPlaylistAuthPermanent) {
			t.Errorf("status %d: retryable auth must not wrap ErrPlaylistAuthPermanent", status)
		}
	}
}

func TestFetchAndParse_AuthPermanent(t *testing.T) {
	srv := playlistServer(t, http.StatusForbidden, `{"error_code":"subscriber_only"}`)
	var sawStatus int
	var sawBody []byte
	p := &Poller{
		URL:        srv.URL,
		HTTPClient: srv.Client(),
		ClassifyAuth: func(status int, body []byte) bool {
			sawStatus = status
			sawBody = body
			return true
		},
	}
	_, err := p.fetchAndParse(context.Background())
	if !errors.Is(err, ErrPlaylistAuthPermanent) {
		t.Fatalf("err=%v, want ErrPlaylistAuthPermanent", err)
	}
	if errors.Is(err, ErrPlaylistAuth) {
		t.Error("permanent auth must not also wrap ErrPlaylistAuth")
	}
	if sawStatus != http.StatusForbidden {
		t.Errorf("ClassifyAuth saw status %d, want 403", sawStatus)
	}
	if !strings.Contains(string(sawBody), "subscriber_only") {
		t.Errorf("ClassifyAuth body=%q, want it to carry the response body", sawBody)
	}
}

func TestFetchAndParse_ClassifyAuthRetryableWhenNotPermanent(t *testing.T) {
	// ClassifyAuth present but returns false → fall through to the
	// retryable sentinel, not the permanent one.
	srv := playlistServer(t, http.StatusUnauthorized, "transient")
	p := &Poller{
		URL:          srv.URL,
		HTTPClient:   srv.Client(),
		ClassifyAuth: func(int, []byte) bool { return false },
	}
	_, err := p.fetchAndParse(context.Background())
	if !errors.Is(err, ErrPlaylistAuth) {
		t.Fatalf("err=%v, want ErrPlaylistAuth", err)
	}
	if errors.Is(err, ErrPlaylistAuthPermanent) {
		t.Error("ClassifyAuth=false must not yield ErrPlaylistAuthPermanent")
	}
}

func TestFetchAndParse_Gone(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		srv := playlistServer(t, status, "gone")
		p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}
		_, err := p.fetchAndParse(context.Background())
		if !errors.Is(err, ErrPlaylistGone) {
			t.Errorf("status %d: err=%v, want ErrPlaylistGone", status, err)
		}
	}
}

func TestFetchAndParse_OtherStatusIsPlainError(t *testing.T) {
	srv := playlistServer(t, http.StatusInternalServerError, "boom")
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}
	_, err := p.fetchAndParse(context.Background())
	if err == nil {
		t.Fatal("expected error for 500")
	}
	// A 5xx is a transient retry candidate, not one of the typed
	// terminal sentinels.
	for _, sentinel := range []error{ErrPlaylistAuth, ErrPlaylistAuthPermanent, ErrPlaylistGone} {
		if errors.Is(err, sentinel) {
			t.Errorf("500 must not wrap %v", sentinel)
		}
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err=%q, want it to mention status 500", err)
	}
}

func TestFetchAndParse_ParseRejectBubblesUp(t *testing.T) {
	// A 200 body the capability gate rejects (AES-128) surfaces the
	// parser's error through fetchAndParse unchanged.
	body := "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-TARGETDURATION:2\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"https://e/key\",IV=0x12345678901234567890123456789012\n" +
		"#EXTINF:2.000,\nhttps://e/0.ts\n"
	srv := playlistServer(t, http.StatusOK, body)
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}
	_, err := p.fetchAndParse(context.Background())
	if !errors.Is(err, ErrUnsupportedManifest) {
		t.Fatalf("err=%v, want ErrUnsupportedManifest", err)
	}
}

func TestFetchAndParse_ResolveURIError(t *testing.T) {
	// A syntactically broken segment URI passes the M3U8 decoder but
	// fails url.Parse during resolution.
	body := "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-TARGETDURATION:2\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:2.000,\nhttp://e/%zz.ts\n"
	srv := playlistServer(t, http.StatusOK, body)
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}
	_, err := p.fetchAndParse(context.Background())
	if err == nil || !strings.Contains(err.Error(), "resolve URIs") {
		t.Fatalf("err=%v, want a resolve URIs error", err)
	}
}

func TestFetchAndParse_BadRequestURL(t *testing.T) {
	// A control character in the URL fails http.NewRequestWithContext
	// before any transport call.
	p := &Poller{URL: "http://\x7fhost/playlist", HTTPClient: http.DefaultClient}
	_, err := p.fetchAndParse(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed request URL")
	}
}

func TestFetchAndParse_TransportError(t *testing.T) {
	// Stand up a server then close it so the dial fails.
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	client := srv.Client()
	srv.Close()
	p := &Poller{URL: addr + "/playlist", HTTPClient: client}
	_, err := p.fetchAndParse(context.Background())
	if err == nil {
		t.Fatal("expected transport error against closed server")
	}
}

// --- resolveURIs -------------------------------------------------

func TestResolveURIs_RelativeAbsoluteAndEmpty(t *testing.T) {
	pl := &MediaPlaylist{
		Segments: []Segment{
			{URI: "seg0.ts"},                  // relative → resolved
			{URI: "https://cdn.example/9.ts"}, // absolute → untouched
			{URI: ""},                         // empty → stays empty
		},
	}
	if err := resolveURIs(pl, "https://edge.example/v/index.m3u8"); err != nil {
		t.Fatalf("resolveURIs: %v", err)
	}
	if got, want := pl.Segments[0].URI, "https://edge.example/v/seg0.ts"; got != want {
		t.Errorf("relative resolved to %q, want %q", got, want)
	}
	if got, want := pl.Segments[1].URI, "https://cdn.example/9.ts"; got != want {
		t.Errorf("absolute changed to %q, want %q", got, want)
	}
	if pl.Segments[2].URI != "" {
		t.Errorf("empty URI became %q, want empty", pl.Segments[2].URI)
	}
}

func TestResolveURIs_InitSegmentResolved(t *testing.T) {
	pl := &MediaPlaylist{
		Init:     &InitSegment{URI: "init.mp4"},
		Segments: []Segment{{URI: "0.m4s"}},
	}
	if err := resolveURIs(pl, "https://edge.example/v/index.m3u8"); err != nil {
		t.Fatalf("resolveURIs: %v", err)
	}
	if got, want := pl.Init.URI, "https://edge.example/v/init.mp4"; got != want {
		t.Errorf("Init.URI=%q, want %q", got, want)
	}
}

func TestResolveURIs_BadBase(t *testing.T) {
	pl := &MediaPlaylist{Segments: []Segment{{URI: "0.ts"}}}
	if err := resolveURIs(pl, ":bad"); err == nil {
		t.Fatal("expected error for unparseable base URL")
	}
}

func TestResolveURIs_BadSegmentURI(t *testing.T) {
	pl := &MediaPlaylist{Segments: []Segment{{URI: "\x7f"}}}
	if err := resolveURIs(pl, "https://edge.example/v/index.m3u8"); err == nil {
		t.Fatal("expected error for unparseable segment URI")
	}
}

func TestResolveURIs_BadInitURI(t *testing.T) {
	pl := &MediaPlaylist{Init: &InitSegment{URI: "\x7f"}}
	if err := resolveURIs(pl, "https://edge.example/v/index.m3u8"); err == nil {
		t.Fatal("expected error for unparseable init URI")
	}
}

// --- truncateForLog ----------------------------------------------

func TestTruncateForLog(t *testing.T) {
	short := []byte("a tidy little body")
	if got := truncateForLog(short); got != string(short) {
		t.Errorf("short body=%q, want unchanged", got)
	}

	long := []byte(strings.Repeat("x", 250))
	got := truncateForLog(long)
	if want := strings.Repeat("x", 200) + "…"; got != want {
		t.Errorf("long body truncated to %d runes, want 200+ellipsis", len([]rune(got)))
	}

	// Exactly at the limit is not truncated.
	exact := []byte(strings.Repeat("y", 200))
	if got := truncateForLog(exact); got != string(exact) {
		t.Error("200-byte body must not be truncated")
	}
}

// --- segmentExt --------------------------------------------------

func TestSegmentExt(t *testing.T) {
	if got := segmentExt(SegmentKindFMP4); got != ".m4s" {
		t.Errorf("fmp4 ext=%q, want .m4s", got)
	}
	if got := segmentExt(SegmentKindTS); got != ".ts" {
		t.Errorf("ts ext=%q, want .ts", got)
	}
	// Anything that isn't fmp4 falls back to .ts.
	if got := segmentExt(SegmentKind("weird")); got != ".ts" {
		t.Errorf("unknown ext=%q, want .ts fallback", got)
	}
}

// --- sleepCtx ----------------------------------------------------

func TestSleepCtx_NonPositiveReturnsImmediately(t *testing.T) {
	for _, d := range []time.Duration{0, -5 * time.Millisecond} {
		if err := sleepCtx(context.Background(), d); err != nil {
			t.Errorf("sleepCtx(%v)=%v, want nil", d, err)
		}
	}
}

func TestSleepCtx_TimerFires(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleepCtx=%v, want nil after timer", err)
	}
}

func TestSleepCtx_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx=%v, want context.Canceled", err)
	}
}

// --- isCanceled --------------------------------------------------

func TestIsCanceled(t *testing.T) {
	bg := context.Background()
	canceled, cancel := context.WithCancel(bg)
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"err is Canceled", bg, context.Canceled, true},
		{"err is DeadlineExceeded", bg, context.DeadlineExceeded, true},
		{"ctx canceled, transport err", canceled, io.EOF, true},
		{"live ctx, transport err", bg, io.EOF, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCanceled(tc.ctx, tc.err); got != tc.want {
				t.Errorf("isCanceled=%v, want %v", got, tc.want)
			}
		})
	}
}

// --- Run: deterministic single-poll / terminal paths -------------
//
// These drive Run to a definite termination (ENDLIST, retry budget
// exhausted, or a pre-canceled context) so there is no reliance on
// poll-tick timing. The mid-loop "ctx.Done during a channel send"
// branches are deliberately not exercised here: forcing them needs a
// blocked send racing a cancel, which is the flaky-over-the-loop
// shape this package avoids. See the extraction notes for how those
// would become unit-testable.

const vodPlaylist = "#EXTM3U\n" +
	"#EXT-X-VERSION:3\n" +
	"#EXT-X-TARGETDURATION:2\n" +
	"#EXT-X-MEDIA-SEQUENCE:0\n" +
	"#EXTINF:2.000,\nhttps://edge.example/0.ts\n" +
	"#EXTINF:2.000,\nhttps://edge.example/1.ts\n" +
	"#EXTINF:2.000,\nhttps://edge.example/2.ts\n" +
	"#EXT-X-ENDLIST\n"

func TestPollerRun_EndListNormalizesDefaultsAndEmits(t *testing.T) {
	srv := playlistServer(t, http.StatusOK, vodPlaylist)
	// Leave Log + HTTPClient zero so Run's normalization path runs;
	// http.DefaultClient reaches the httptest server fine.
	p := &Poller{URL: srv.URL}

	pr, gotFirst, jobs, err := runPollerCollect(context.Background(), p)
	if err != nil {
		t.Fatalf("Run=%v, want nil on ENDLIST", err)
	}
	if !gotFirst {
		t.Fatal("expected a PollResult on the first poll")
	}
	if pr.Kind != SegmentKindTS || pr.TargetDuration != 2*time.Second {
		t.Errorf("PollResult=%+v, want ts/2s", pr)
	}
	if len(jobs) != 3 {
		t.Fatalf("emitted %d jobs, want 3", len(jobs))
	}
	for i, want := range []string{"0.ts", "1.ts", "2.ts"} {
		if jobs[i].FinalName != want {
			t.Errorf("jobs[%d].FinalName=%q, want %q", i, jobs[i].FinalName, want)
		}
		if jobs[i].TargetDuration != 2*time.Second {
			t.Errorf("jobs[%d].TargetDuration=%v, want 2s", i, jobs[i].TargetDuration)
		}
	}
}

func TestPollerRun_TransientRetryExhausted(t *testing.T) {
	srv := playlistServer(t, http.StatusInternalServerError, "boom")
	p := &Poller{
		URL:         srv.URL,
		HTTPClient:  srv.Client(),
		MaxAttempts: 3,
		BackoffBase: time.Millisecond,
		BackoffMax:  2 * time.Millisecond,
		MinTick:     time.Millisecond,
	}
	pr, gotFirst, jobs, err := runPollerCollect(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "exhausted after 3 attempts") {
		t.Fatalf("Run=%v, want exhausted-after-3 error", err)
	}
	if gotFirst {
		t.Errorf("no PollResult expected when every poll fails, got %+v", pr)
	}
	if len(jobs) != 0 {
		t.Errorf("emitted %d jobs, want 0", len(jobs))
	}
}

func TestPollerRun_ContextCanceledDuringFetch(t *testing.T) {
	srv := playlistServer(t, http.StatusOK, vodPlaylist)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before Run's first fetch
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}

	_, gotFirst, jobs, err := runPollerCollect(ctx, p)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run=%v, want context.Canceled", err)
	}
	if gotFirst || len(jobs) != 0 {
		t.Errorf("canceled run must emit nothing: gotFirst=%v jobs=%d", gotFirst, len(jobs))
	}
}

func TestPollerRun_AuthErrorBubbles(t *testing.T) {
	srv := playlistServer(t, http.StatusForbidden, "no")
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}
	_, _, _, err := runPollerCollect(context.Background(), p)
	if !errors.Is(err, ErrPlaylistAuth) {
		t.Fatalf("Run=%v, want ErrPlaylistAuth (no in-place retry)", err)
	}
}

func TestPollerRun_GoneErrorBubbles(t *testing.T) {
	srv := playlistServer(t, http.StatusNotFound, "gone")
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client()}
	_, _, _, err := runPollerCollect(context.Background(), p)
	if !errors.Is(err, ErrPlaylistGone) {
		t.Fatalf("Run=%v, want ErrPlaylistGone", err)
	}
}
