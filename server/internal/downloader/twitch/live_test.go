//go:build live

// Live tests hit real gql.twitch.tv and usher.ttvnw.net. Excluded
// from the default `go test ./...` run — enable via:
//
//	go test -tags live -run Live ./internal/downloader/twitch/...
//	# or: task test-live
//
// Pick the target channel with TWITCH_LIVE_CHANNEL (default: a live
// channel at the time of writing; override to something currently
// live if the default has gone offline). These tests probe the live
// API surface — they will fail if Twitch changes the shape of the
// response, which is exactly what we want.

package twitch

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func liveChannel() string {
	if c := os.Getenv("TWITCH_LIVE_CHANNEL"); c != "" {
		return c
	}
	return "tumblurr"
}

// skipIfOffline turns "channel happens not to be live right now"
// into a clean Skip instead of a failure. Usher returns 404 with
// "Can not find channel" for offline streams; that error is noise
// from the test-design perspective — we want these tests to fail
// loud when our code is broken and stay quiet when the real world
// just moved on. Any other error still fatally fails the test so
// real regressions (wrong URL shape, broken auth, etc.) surface.
func skipIfOffline(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var ae *AuthError
	if errors.As(err, &ae) && ae.Status == 404 && strings.Contains(ae.Message, "Can not find channel") {
		t.Skipf("channel %q is offline (usher 404) — not a real failure; set TWITCH_LIVE_CHANNEL to a live channel to exercise this test", liveChannel())
	}
	// Not the offline marker — real failure, let it surface.
	t.Fatalf("%v", err)
}

func newLiveClient(t *testing.T) *Client {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return New(Config{}, log)
}

// TestLive_PlaybackToken exercises Stage 1 against real Twitch. A
// pass proves our variables + headers + Origin still match what the
// web client sends — any future "server error" regression would
// show up here before it ships.
func TestLive_PlaybackToken(t *testing.T) {
	ch := liveChannel()
	c := newLiveClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tok, err := c.PlaybackToken(ctx, ch, "")
	if err != nil {
		t.Fatalf("PlaybackToken(%q): %v", ch, err)
	}
	if tok.Value == "" || tok.Signature == "" {
		t.Fatalf("empty token: value=%q signature=%q", tok.Value, tok.Signature)
	}
	// The token value is a JSON blob; a trivial sniff confirms we
	// got a playback token rather than some other response shape
	// Twitch might return for an offline / unknown channel.
	if !strings.Contains(tok.Value, `"channel":"`+ch+`"`) {
		t.Errorf("token value does not reference channel %q\nvalue=%s", ch, tok.Value)
	}
	t.Logf("ok: token %d bytes, signature %s…", len(tok.Value), tok.Signature[:12])
}

// TestLive_FetchMasterPlaylist runs Stage 1 + Stage 2 end-to-end:
// token → usher → parsed manifest. Passes only when the signed URL
// actually authorizes the usher call, which means this catches the
// whole "did we shape the token request correctly" question,
// including the Stage 2 query parameters.
func TestLive_FetchMasterPlaylist(t *testing.T) {
	ch := liveChannel()
	c := newLiveClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tok, err := c.PlaybackToken(ctx, ch, "")
	if err != nil {
		t.Fatalf("PlaybackToken: %v", err)
	}
	m, err := c.FetchMasterPlaylist(ctx, ch, tok, SelectOptions{})
	skipIfOffline(t, err)
	if len(m.Variants) == 0 {
		t.Fatal("no variants in master playlist")
	}
	t.Logf("ok: %d variants", len(m.Variants))
	for _, v := range m.Variants {
		t.Logf("  quality=%-11s codec=%-5s fps=%.0f group=%s", v.Quality, v.Codec, v.FPS, v.GroupID)
	}
}

// TestLive_SelectVariant proves Stages 1-3 compose against the real
// API for the default "video, 1080, no force_h264" job shape.
// Useful when investigating a specific channel's offered ladder —
// set TWITCH_LIVE_CHANNEL and read the log.
func TestLive_SelectVariant(t *testing.T) {
	ch := liveChannel()
	c := newLiveClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tok, err := c.PlaybackToken(ctx, ch, "")
	if err != nil {
		t.Fatalf("PlaybackToken: %v", err)
	}
	opts := SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
	}
	m, err := c.FetchMasterPlaylist(ctx, ch, tok, opts)
	skipIfOffline(t, err)
	sel, err := SelectVariant(m, opts)
	if err != nil {
		t.Fatalf("SelectVariant: %v", err)
	}
	t.Logf("selected: quality=%s codec=%s", sel.Quality, sel.Codec)
	if sel.URL == "" {
		t.Fatal("selected variant has empty URL")
	}
}
