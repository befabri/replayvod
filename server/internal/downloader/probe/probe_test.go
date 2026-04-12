package probe

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockRunner writes stdout from a canned string and returns a
// canned error. Used to drive probe tests without a real
// ffprobe binary.
type mockRunner struct {
	stdout    string
	stderr    string
	returnErr error
	lastArgs  []string
}

func (m *mockRunner) Run(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	m.lastArgs = append([]string{}, args...)
	if m.stdout != "" {
		_, _ = io.WriteString(stdout, m.stdout)
	}
	if m.stderr != "" {
		_, _ = io.WriteString(stderr, m.stderr)
	}
	return m.returnErr
}

// writeFile creates a file we can os.Stat. ffprobe output is
// mocked; we just need a real file for the Size field.
func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

const sampleVideoOutput = `{
  "streams": [
    {"codec_name": "h264", "codec_type": "video", "duration": "124.500000", "width": 1920, "height": 1080},
    {"codec_name": "aac", "codec_type": "audio", "duration": "124.470000", "sample_rate": "48000"}
  ],
  "format": {
    "duration": "124.500000"
  }
}`

const sampleAudioOnlyOutput = `{
  "streams": [
    {"codec_name": "aac", "codec_type": "audio", "duration": "60.000000", "sample_rate": "48000"}
  ],
  "format": {
    "duration": "60.000000"
  }
}`

func TestProbe_VideoAndAudio(t *testing.T) {
	path := writeFile(t, "fake mp4 bytes for size stat")
	m := &mockRunner{stdout: sampleVideoOutput}
	p := &Probe{Runner: m, Log: slog.New(slog.DiscardHandler)}

	result, err := p.Run(context.Background(), path)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Duration != 124.5 {
		t.Errorf("Duration=%v, want 124.5", result.Duration)
	}
	if result.VideoStream == nil {
		t.Fatal("VideoStream nil")
	}
	if result.VideoStream.Codec != "h264" {
		t.Errorf("video codec=%s", result.VideoStream.Codec)
	}
	if result.VideoStream.Width != 1920 || result.VideoStream.Height != 1080 {
		t.Errorf("video dims=%dx%d, want 1920x1080", result.VideoStream.Width, result.VideoStream.Height)
	}
	if result.AudioStream == nil {
		t.Fatal("AudioStream nil")
	}
	if result.AudioStream.Codec != "aac" {
		t.Errorf("audio codec=%s", result.AudioStream.Codec)
	}
	if result.AudioStream.SampleRate != 48000 {
		t.Errorf("sample rate=%d", result.AudioStream.SampleRate)
	}
	if result.Size != int64(len("fake mp4 bytes for size stat")) {
		t.Errorf("Size=%d, want file length", result.Size)
	}
}

func TestProbe_AudioOnly(t *testing.T) {
	path := writeFile(t, "x")
	m := &mockRunner{stdout: sampleAudioOnlyOutput}
	p := &Probe{Runner: m}

	result, err := p.Run(context.Background(), path)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.VideoStream != nil {
		t.Errorf("VideoStream=%+v, want nil for audio-only", result.VideoStream)
	}
	if result.AudioStream == nil {
		t.Fatal("AudioStream nil on audio-only file")
	}
}

func TestProbe_ArgShape(t *testing.T) {
	// Pin the ffprobe invocation shape — log scrapers key on
	// `-print_format json` to find probe output.
	path := writeFile(t, "x")
	m := &mockRunner{stdout: sampleVideoOutput}
	p := &Probe{Runner: m}
	_, _ = p.Run(context.Background(), path)

	got := strings.Join(m.lastArgs, " ")
	for _, needle := range []string{
		"-v error",
		"-print_format json",
		"-show_format",
		"-show_streams",
	} {
		if !strings.Contains(got, needle) {
			t.Errorf("args missing %q: %s", needle, got)
		}
	}
}

func TestProbe_FFprobeFailureIncludesStderr(t *testing.T) {
	path := writeFile(t, "x")
	m := &mockRunner{
		returnErr: errors.New("exit status 1"),
		stderr:    "Invalid data found when processing input",
	}
	p := &Probe{Runner: m}

	_, err := p.Run(context.Background(), path)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "Invalid data found") {
		t.Errorf("err=%v, want stderr excerpt", err)
	}
}

func TestProbe_MissingFile(t *testing.T) {
	m := &mockRunner{stdout: sampleVideoOutput}
	p := &Probe{Runner: m}
	_, err := p.Run(context.Background(), "/no/such/file")
	if err == nil {
		t.Fatal("want stat error")
	}
}

func TestProbe_MalformedJSON(t *testing.T) {
	path := writeFile(t, "x")
	m := &mockRunner{stdout: "{not valid json"}
	p := &Probe{Runner: m}
	_, err := p.Run(context.Background(), path)
	if err == nil {
		t.Fatal("want decode error")
	}
}

func TestProbe_CtxCancelPassesThrough(t *testing.T) {
	path := writeFile(t, "x")
	m := &mockRunner{returnErr: context.Canceled}
	p := &Probe{Runner: m}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Run(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v, want context.Canceled", err)
	}
}

func TestParseProbeOutput_StreamDurationNAIsZero(t *testing.T) {
	// Some remuxed containers emit "N/A" for stream duration.
	// The parser must not error; the caller's corruption
	// check treats 0 as "can't measure" and skips healing.
	data := []byte(`{
		"streams": [{"codec_name": "h264", "codec_type": "video", "duration": "N/A", "width": 1280, "height": 720}],
		"format": {"duration": "30.0"}
	}`)
	r, err := parseProbeOutput(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.VideoStream == nil {
		t.Fatal("VideoStream nil")
	}
	if r.VideoStream.Duration != 0 {
		t.Errorf("stream duration=%v, want 0 for N/A", r.VideoStream.Duration)
	}
	if r.Duration != 30.0 {
		t.Errorf("format duration=%v", r.Duration)
	}
}

func TestParseProbeOutput_EmptyStreams(t *testing.T) {
	data := []byte(`{"streams": [], "format": {"duration": "0.0"}}`)
	r, err := parseProbeOutput(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.VideoStream != nil || r.AudioStream != nil {
		t.Error("expected both streams nil")
	}
}
