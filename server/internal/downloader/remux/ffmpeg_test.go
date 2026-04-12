package remux

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// mockRunner captures the args passed to Run + lets the test
// script a return error. When emulateSuccess is true, the runner
// writes an empty file at args[len(args)-1] before returning so
// Run's .part → final rename step has something to rename.
type mockRunner struct {
	lastName        string
	lastArgs        []string
	stderrOut       string
	returnErr       error
	returnDelay     time.Duration
	emulateSuccess  bool
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
	if m.returnErr == nil && m.emulateSuccess && len(args) > 0 {
		out := args[len(args)-1]
		if err := os.WriteFile(out, []byte{}, 0o644); err != nil {
			return err
		}
	}
	return m.returnErr
}

func TestFFmpegArgs_TS(t *testing.T) {
	in := RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/work/segments.txt",
		OutputDir:      "/out",
		OutputBasename: "rec-part01",
	}
	args, err := ffmpegArgs(in, in.OutputPath()+partSuffix)
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	want := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", "/work/segments.txt",
		"-c", "copy",
		"/out/rec-part01.mp4.part",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant=%v", args, want)
	}
}

func TestFFmpegArgs_FMP4_Audio(t *testing.T) {
	in := RunInput{
		Mode:           ModeFMP4,
		Kind:           KindAudio,
		InputPath:      "/work/media.m3u8",
		OutputDir:      "/out",
		OutputBasename: "rec-part01",
	}
	args, err := ffmpegArgs(in, in.OutputPath()+partSuffix)
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	want := []string{
		"-y",
		"-i", "/work/media.m3u8",
		"-c", "copy",
		"/out/rec-part01.m4a.part",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant=%v", args, want)
	}
}

func TestFFmpegArgs_UnknownMode(t *testing.T) {
	_, err := ffmpegArgs(RunInput{Mode: Mode("x")}, "/tmp/out.mp4.part")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRemuxer_Run_SuccessCommitsAtomicRename(t *testing.T) {
	// After success: final file exists, .part file doesn't.
	dir := t.TempDir()
	m := &mockRunner{emulateSuccess: true}
	r := &Remuxer{Runner: m}

	err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      dir,
		OutputBasename: "rec",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rec.mp4")); err != nil {
		t.Errorf("final file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rec.mp4.part")); !os.IsNotExist(err) {
		t.Errorf(".part should be gone after commit, got err=%v", err)
	}
	// Runner was invoked with the .part path as the output arg.
	if m.lastArgs[len(m.lastArgs)-1] != filepath.Join(dir, "rec.mp4.part") {
		t.Errorf("last arg=%q, want .part path", m.lastArgs[len(m.lastArgs)-1])
	}
}

func TestRemuxer_Run_CustomBinary(t *testing.T) {
	dir := t.TempDir()
	m := &mockRunner{emulateSuccess: true}
	r := &Remuxer{Runner: m, FFmpegPath: "/usr/local/bin/ffmpeg-hevc"}
	_ = r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      dir,
		OutputBasename: "rec",
	})
	if m.lastName != "/usr/local/bin/ffmpeg-hevc" {
		t.Errorf("custom binary not used: got %q", m.lastName)
	}
}

func TestRemuxer_Run_FailureCleansPartFile(t *testing.T) {
	// On ffmpeg failure the .part file — if ffmpeg happened to
	// create one before crashing — must be removed, and no
	// final file may be produced.
	dir := t.TempDir()
	// Pretend ffmpeg wrote a partial .part file before crashing:
	// create it ourselves, then have the runner return error.
	partPath := filepath.Join(dir, "rec.mp4.part")
	if err := os.WriteFile(partPath, []byte("partial junk"), 0o644); err != nil {
		t.Fatalf("seed .part: %v", err)
	}
	m := &mockRunner{
		returnErr: errors.New("exit status 1"),
		stderrOut: "ffmpeg crashed",
	}
	r := &Remuxer{Runner: m}
	err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      dir,
		OutputBasename: "rec",
	})
	if err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Errorf(".part not cleaned after ffmpeg failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rec.mp4")); !os.IsNotExist(err) {
		t.Errorf("final should not exist on failure, err=%v", err)
	}
}

// panickyRunner writes a partial .part file like ffmpeg would,
// then panics — simulates a hard fault between runner.Run
// returning and the rename step. The deferred cleanup in Run
// should remove the .part even though the panic propagates.
type panickyRunner struct{}

func (panickyRunner) Run(_ context.Context, _ string, args []string, _ io.Writer) error {
	if len(args) > 0 {
		_ = os.WriteFile(args[len(args)-1], []byte("in flight"), 0o644)
	}
	panic("simulated fault")
}

func TestRemuxer_Run_PanicMidRunStillCleansPartFile(t *testing.T) {
	dir := t.TempDir()
	r := &Remuxer{Runner: panickyRunner{}}

	defer func() {
		// Swallow the panic so the test can assert state after.
		if recover() == nil {
			t.Fatal("expected panic from runner")
		}
		if _, err := os.Stat(filepath.Join(dir, "rec.mp4.part")); !os.IsNotExist(err) {
			t.Errorf(".part not cleaned after panic: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "rec.mp4")); !os.IsNotExist(err) {
			t.Errorf("final file should not exist after panic: %v", err)
		}
	}()

	_ = r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      dir,
		OutputBasename: "rec",
	})
}

func TestRemuxer_Run_CtxCancelCleansPartFile(t *testing.T) {
	dir := t.TempDir()
	partPath := filepath.Join(dir, "rec.mp4.part")
	_ = os.WriteFile(partPath, []byte("in flight"), 0o644)

	m := &mockRunner{returnErr: context.Canceled}
	r := &Remuxer{Runner: m}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Run(ctx, RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      dir,
		OutputBasename: "rec",
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), "stderr") {
		t.Errorf("ctx cancel dressed with stderr: %q", err)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Errorf(".part not cleaned after cancel: %v", err)
	}
}

func TestRemuxer_Run_FailureIncludesStderrPreview(t *testing.T) {
	dir := t.TempDir()
	m := &mockRunner{
		returnErr: errors.New("exit status 1"),
		stderrOut: "this is some ffmpeg stderr output",
	}
	r := &Remuxer{Runner: m}
	err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      "/in/segments.txt",
		OutputDir:      dir,
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
	dir := t.TempDir()
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
		OutputDir:      dir,
		OutputBasename: "rec",
	})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("err=%v, want truncation marker", err)
	}
	if len(err.Error()) > 12<<10 {
		t.Errorf("err length=%d, want < 12 KB", len(err.Error()))
	}
}

func TestRemuxer_Run_UnknownModeFailsEarly(t *testing.T) {
	// Unknown mode must fail before invoking ffmpeg.
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
		OutputDir:      "/out",
		OutputBasename: "rec-part01",
	}
	if got := in.OutputPath(); got != "/out/rec-part01.mp4" {
		t.Errorf("OutputPath=%q", got)
	}
	in.Kind = KindAudio
	if got := in.OutputPath(); got != "/out/rec-part01.m4a" {
		t.Errorf("OutputPath audio=%q", got)
	}
	// Trailing slash on OutputDir normalized.
	in.OutputDir = "/out/"
	in.Kind = KindVideo
	if got := in.OutputPath(); got != "/out/rec-part01.mp4" {
		t.Errorf("OutputPath trailing slash=%q", got)
	}
}
