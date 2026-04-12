package remux

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// mockRunner captures the args passed to Run + lets the test
// script a return error. Test-only.
type mockRunner struct {
	lastName   string
	lastArgs   []string
	stderrOut  string
	returnErr  error
	returnDelay time.Duration
}

func (m *mockRunner) Run(ctx context.Context, name string, args []string, stderr io.Writer) error {
	m.lastName = name
	m.lastArgs = append([]string{}, args...)
	if m.stderrOut != "" {
		_, _ = io.WriteString(stderr, m.stderrOut)
	}
	if m.returnDelay > 0 {
		select {
		case <-time.After(m.returnDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.returnErr
}

func TestFFmpegArgs_TS(t *testing.T) {
	args, err := ffmpegArgs(RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/work/segments.txt",
		OutputDir:      "/out",
		OutputBasename: "rec-part01",
	})
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	want := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", "/work/segments.txt",
		"-c", "copy",
		"/out/rec-part01.mp4",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant=%v", args, want)
	}
}

func TestFFmpegArgs_FMP4_Audio(t *testing.T) {
	args, err := ffmpegArgs(RunInput{
		Mode:           ModeFMP4,
		Kind:           KindAudio,
		InputPath:      "/work/media.m3u8",
		OutputDir:      "/out",
		OutputBasename: "rec-part01",
	})
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	want := []string{
		"-y",
		"-i", "/work/media.m3u8",
		"-c", "copy",
		"/out/rec-part01.m4a",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant=%v", args, want)
	}
}

func TestFFmpegArgs_UnknownMode(t *testing.T) {
	_, err := ffmpegArgs(RunInput{Mode: Mode("x")})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRemuxer_Run_SuccessHitsRunner(t *testing.T) {
	m := &mockRunner{}
	r := &Remuxer{Runner: m}
	err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      "/out",
		OutputBasename: "rec",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if m.lastName != DefaultFFmpegPath {
		t.Errorf("binary=%q, want %q", m.lastName, DefaultFFmpegPath)
	}
	if len(m.lastArgs) == 0 {
		t.Fatal("runner got no args")
	}
}

func TestRemuxer_Run_CustomBinary(t *testing.T) {
	m := &mockRunner{}
	r := &Remuxer{Runner: m, FFmpegPath: "/usr/local/bin/ffmpeg-hevc"}
	_ = r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      "/out",
		OutputBasename: "rec",
	})
	if m.lastName != "/usr/local/bin/ffmpeg-hevc" {
		t.Errorf("custom binary not used: got %q", m.lastName)
	}
}

func TestRemuxer_Run_FailureIncludesStderrPreview(t *testing.T) {
	m := &mockRunner{
		returnErr: errors.New("exit status 1"),
		stderrOut: "this is some ffmpeg stderr output",
	}
	r := &Remuxer{Runner: m}
	err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      "/out",
		OutputBasename: "rec",
	})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "ffmpeg stderr output") {
		t.Errorf("err=%v, want stderr excerpt", err)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("err=%v, want underlying error", err)
	}
}

func TestRemuxer_Run_FailureTruncatesStderr(t *testing.T) {
	// Long stderr must be truncated to 8 KB so the error
	// message stays loggable.
	big := strings.Repeat("A", 16<<10)
	m := &mockRunner{
		returnErr: errors.New("exit status 1"),
		stderrOut: big,
	}
	r := &Remuxer{Runner: m}
	err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      "/out",
		OutputBasename: "rec",
	})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("err=%v, want truncation marker", err)
	}
	// The returned message must be bounded — 8 KB preview + a
	// bit of framing text. 12 KB is a generous upper bound.
	if len(err.Error()) > 12<<10 {
		t.Errorf("err length=%d, want < 12 KB", len(err.Error()))
	}
}

func TestRemuxer_Run_CtxCancelPassesThrough(t *testing.T) {
	m := &mockRunner{
		returnErr:   context.Canceled,
		returnDelay: 50 * time.Millisecond,
	}
	r := &Remuxer{Runner: m}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	err := r.Run(ctx, RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      "/out",
		OutputBasename: "rec",
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v, want context.Canceled surfaced cleanly", err)
	}
	// Cancel must NOT be dressed up with ffmpeg stderr — the
	// caller already knows the cancel was intentional.
	if strings.Contains(err.Error(), "stderr") {
		t.Errorf("err=%q, should not include stderr for ctx cancel", err.Error())
	}
}

func TestRemuxer_Run_UnknownModeFailsEarly(t *testing.T) {
	// Unknown mode must fail before invoking ffmpeg. Otherwise
	// a typo in caller config could produce an "exit status 1"
	// that looks like an ffmpeg problem.
	m := &mockRunner{}
	r := &Remuxer{Runner: m}
	err := r.Run(context.Background(), RunInput{Mode: Mode("bogus"), Kind: KindVideo})
	if err == nil {
		t.Fatal("want error")
	}
	if m.lastName != "" {
		t.Error("runner was invoked on unknown mode — should fail early")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Errorf("short string=%q, want unchanged", got)
	}
	got := truncate(strings.Repeat("x", 50), 10)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("long string missing suffix: %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("x", 10)) {
		t.Errorf("long string missing prefix: %q", got)
	}
}

func TestRunInput_OutputPath(t *testing.T) {
	in := RunInput{
		Kind:           KindVideo,
		OutputDir:      "/out/",
		OutputBasename: "rec-part01",
	}
	if got := in.OutputPath(); got != "/out/rec-part01.mp4" {
		t.Errorf("OutputPath=%q", got)
	}
	in.Kind = KindAudio
	if got := in.OutputPath(); got != "/out/rec-part01.m4a" {
		t.Errorf("OutputPath audio=%q", got)
	}
}
