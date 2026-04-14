//go:build ffmpeg

// Real-ffmpeg tests for the sprite-strip path. Same build-tag
// convention as remux/ffmpeg_real_test.go — invoke via
//
//	go test -tags ffmpeg -count=1 -run TestReal ./internal/downloader/thumbnail/...
//	# or: task test-ffmpeg  (covers all ffmpeg-tagged tests)
//
// These tests exist because the argv-equality unit tests don't
// catch the interesting failures (filter-chain typos, ffmpeg
// version regressions on the `tile` filter, wrong `-f` choice for
// the sprite output path). Each test generates a synthetic MP4,
// runs GenerateStrip against it, and ffprobes the output to
// verify the sprite is a valid JPEG of the expected shape.

package thumbnail

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}
}

// genMP4 produces a synthetic MP4 with `dur` seconds of animated
// test pattern + sine audio. Used as the sprite-strip input.
func genMP4(t *testing.T, path string, dur float64) {
	t.Helper()
	durStr := strconv.FormatFloat(dur, 'f', 2, 64)
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=15:duration="+durStr,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-f", "mp4",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen mp4: %v\n%s", err, out)
	}
}

// probeImage returns the decoded ffprobe JSON for a JPEG. Used to
// assert the sprite's dimensions — they're the load-bearing
// property (tile filter produced the right grid), not the byte
// size or file path.
type imageProbe struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

func probeImage(t *testing.T, path string) imageProbe {
	t.Helper()
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		path,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v\nstderr: %s", path, err, stderr.String())
	}
	var p imageProbe
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("probe decode: %v\n%s", err, out)
	}
	return p
}

// TestReal_GenerateStrip_Defaults verifies the zero-value defaults:
// 12 frames, 4 columns × 3 rows, 160px wide per frame. Sprite
// dimensions should be 4×160 = 640 wide, 3 rows of proportionally
// scaled height (120 each at testsrc's 4:3 → 360 high).
func TestReal_GenerateStrip_Defaults(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	video := filepath.Join(dir, "rec.mp4")
	genMP4(t, video, 10.0)

	strip := filepath.Join(dir, "rec-strip.jpg")
	g := &Generator{Log: slog.New(slog.DiscardHandler)}
	if err := g.GenerateStrip(context.Background(), StripInput{
		VideoPath:       video,
		OutputPath:      strip,
		DurationSeconds: 10.0,
	}); err != nil {
		t.Fatalf("GenerateStrip: %v", err)
	}
	if _, err := os.Stat(strip); err != nil {
		t.Fatalf("sprite missing: %v", err)
	}

	p := probeImage(t, strip)
	if len(p.Streams) != 1 {
		t.Fatalf("streams=%d, want 1", len(p.Streams))
	}
	s := p.Streams[0]
	if s.CodecName != "mjpeg" {
		t.Errorf("codec_name=%q, want mjpeg", s.CodecName)
	}
	// 4 columns × 160 px = 640. 3 rows × 120 px = 360 (testsrc
	// is 320x240 → 4:3; scaled to 160 wide → 120 high).
	if s.Width != 640 {
		t.Errorf("sprite width=%d, want 640 (4 cols × 160 px)", s.Width)
	}
	if s.Height != 360 {
		t.Errorf("sprite height=%d, want 360 (3 rows × 120 px)", s.Height)
	}
}

// TestReal_GenerateStrip_CustomGrid verifies non-default grid
// sizes propagate correctly into the filter chain — 6 frames in
// 6×1, wider tiles. Catches off-by-one in the rows formula.
func TestReal_GenerateStrip_CustomGrid(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	video := filepath.Join(dir, "rec.mp4")
	genMP4(t, video, 6.0)

	strip := filepath.Join(dir, "rec-strip.jpg")
	g := &Generator{Log: slog.New(slog.DiscardHandler)}
	if err := g.GenerateStrip(context.Background(), StripInput{
		VideoPath:       video,
		OutputPath:      strip,
		DurationSeconds: 6.0,
		Frames:          6,
		Columns:         6,
		FrameWidth:      200,
	}); err != nil {
		t.Fatalf("GenerateStrip: %v", err)
	}

	p := probeImage(t, strip)
	s := p.Streams[0]
	// 6 cols × 200 = 1200 wide; 1 row × 150 = 150 tall.
	if s.Width != 1200 {
		t.Errorf("width=%d, want 1200 (6 × 200)", s.Width)
	}
	if s.Height != 150 {
		t.Errorf("height=%d, want 150 (1 × 150)", s.Height)
	}
}

// TestReal_GenerateStrip_ZeroDuration verifies the early-return
// error path — without duration we can't derive the sample rate,
// and the failure mode must be a clean error rather than an
// ffmpeg invocation that emits a silent zero-byte file.
func TestReal_GenerateStrip_ZeroDuration(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	video := filepath.Join(dir, "rec.mp4")
	genMP4(t, video, 2.0)

	strip := filepath.Join(dir, "rec-strip.jpg")
	g := &Generator{Log: slog.New(slog.DiscardHandler)}
	err := g.GenerateStrip(context.Background(), StripInput{
		VideoPath:       video,
		OutputPath:      strip,
		DurationSeconds: 0,
	})
	if err == nil {
		t.Fatal("expected error for zero duration")
	}
	if _, err := os.Stat(strip); !os.IsNotExist(err) {
		t.Errorf("sprite file should not exist on error, stat err=%v", err)
	}
}
