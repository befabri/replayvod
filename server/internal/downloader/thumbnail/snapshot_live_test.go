//go:build live

// Live test for the snapshot URL template: hits real
// static-cdn.jtvnw.net, verifies a live channel returns a JPEG
// we can actually use. The hermetic snapshot_test.go tests
// everything the URL template does internally; only this test can
// catch a Twitch-side URL-shape drift.
//
// Invoke via:
//
//	go test -tags live -count=1 -v -run Live ./internal/downloader/thumbnail/...
//	# or: task test-live  (runs every live-tagged test)
//
// Pick the target channel with TWITCH_LIVE_CHANNEL (shared with
// the GQL live tests); defaults to a channel that was live at
// test-authoring time.

package thumbnail

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// liveChannel reads the shared TWITCH_LIVE_CHANNEL env var with a
// sensible default. Kept local to this file so the ffmpeg-build
// tag tests never link in networking env-var handling.
func liveChannel() string {
	if c := os.Getenv("TWITCH_LIVE_CHANNEL"); c != "" {
		return c
	}
	return "tumblurr"
}

// liveCaptureWriter captures snapshot bytes for inspection.
// Simpler than the hermetic memWriter — we only care about the
// first capture.
type liveCaptureWriter struct {
	bytes []byte
}

func (w *liveCaptureWriter) WriteSnapshot(_ context.Context, _ int, body io.Reader) error {
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	w.bytes = buf
	return nil
}

// TestLive_SnapshotURLTemplate probes the real CDN. If this test
// fails while the hermetic tests pass, Twitch's URL shape has
// drifted and livePreviewURLTemplate needs updating. If BOTH fail,
// the channel under test isn't live — check TWITCH_LIVE_CHANNEL.
//
// Asserts three things that together prove the bytes are a real
// JPEG of a live stream:
//   - HTTP 200 (Snapshotter would skip on non-200)
//   - JPEG magic bytes (0xFFD8FFE0 or similar) at the start
//   - Size above a floor — Twitch's live previews are 30 KB+;
//     anything smaller is an error page or corrupted response.
func TestLive_SnapshotURLTemplate(t *testing.T) {
	ch := liveChannel()

	// Default HTTPClient (no rewriteTransport) — we want to hit
	// the real static-cdn.jtvnw.net through our production URL
	// template.
	s := NewSnapshotter(SnapshotterConfig{
		// Interval is never reached — we cancel after the first
		// immediate fetch.
		Interval: 10 * time.Minute,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	w := &liveCaptureWriter{}
	done := make(chan int, 1)
	go func() { done <- s.Run(ctx, ch, w) }()

	// Wait for the first tick to land, then stop.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	for len(w.bytes) == 0 {
		select {
		case <-waitCtx.Done():
			t.Fatalf("no snapshot landed within 10s — channel %q may be offline or the CDN URL template is wrong", ch)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	cancel()
	count := <-done

	if count != 1 {
		t.Errorf("count=%d, want exactly 1 (immediate fire, no ticker ticks)", count)
	}

	// JPEG magic: every JPEG starts with 0xFF 0xD8 and ends with
	// 0xFF 0xD9. Start-only check is enough here — real live
	// previews are well-formed.
	if len(w.bytes) < 4 {
		t.Fatalf("body too short: %d bytes", len(w.bytes))
	}
	if !bytes.HasPrefix(w.bytes, []byte{0xFF, 0xD8, 0xFF}) {
		t.Errorf("body does not start with JPEG magic; first 8 bytes=% x", w.bytes[:min(8, len(w.bytes))])
	}

	// Floor: Twitch's 1280x720 JPEGs are typically 80-200 KB. A
	// sub-10KB response would mean we got a placeholder or
	// offline-channel image rather than a real frame.
	if n := len(w.bytes); n < 10_000 {
		t.Errorf("body only %d bytes — expected ≥ 10 KB for a live preview", n)
	}

	t.Logf("ok: %d bytes of JPEG from static-cdn.jtvnw.net for channel=%q", len(w.bytes), ch)
}
