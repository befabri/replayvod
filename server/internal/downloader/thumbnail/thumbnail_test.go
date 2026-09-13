package thumbnail

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type scriptedRunner struct {
	responses []response
	calls     []call
}

type response struct {
	stderr string
	err    error
}

type call struct {
	args []string
}

func (s *scriptedRunner) Run(ctx context.Context, name string, args []string, stderr io.Writer) error {
	i := len(s.calls)
	s.calls = append(s.calls, call{args: append([]string{}, args...)})
	if i >= len(s.responses) {
		return errors.New("scriptedRunner: ran out of responses")
	}
	resp := s.responses[i]
	if resp.stderr != "" {
		_, _ = io.WriteString(stderr, resp.stderr)
	}
	return resp.err
}

const singleColorStderr = "Image is a single color, skipping frame"

func TestGenerate_SucceedsOnFirstTry(t *testing.T) {
	r := &scriptedRunner{
		responses: []response{{err: nil}},
	}
	g := &Generator{Runner: r, Log: slog.New(slog.DiscardHandler)}
	err := g.Generate(context.Background(), Input{
		VideoPath:       "/in.mp4",
		OutputPath:      filepath.Join(t.TempDir(), "out.jpg"),
		DurationSeconds: 100,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("calls=%d, want 1", len(r.calls))
	}
}

func TestGenerate_RetriesOnSingleColor(t *testing.T) {
	r := &scriptedRunner{
		responses: []response{
			{stderr: singleColorStderr, err: errors.New("exit 1")},
			{stderr: singleColorStderr, err: errors.New("exit 1")},
			{err: nil},
		},
	}
	g := &Generator{Runner: r}
	err := g.Generate(context.Background(), Input{
		VideoPath:       "/in.mp4",
		OutputPath:      filepath.Join(t.TempDir(), "out.jpg"),
		DurationSeconds: 100,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("calls=%d, want 3", len(r.calls))
	}
	offsets := make([]string, 0, 3)
	for _, c := range r.calls {
		for i, a := range c.args {
			if a == "-ss" && i+1 < len(c.args) {
				offsets = append(offsets, c.args[i+1])
				break
			}
		}
	}
	want := []string{"10.00", "70.00", "130.00"}
	if !reflect.DeepEqual(offsets, want) {
		t.Errorf("offsets=%v, want %v", offsets, want)
	}
}

func TestGenerate_ExhaustedRetriesReturnsTyped(t *testing.T) {
	resps := make([]response, 5)
	for i := range resps {
		resps[i] = response{stderr: singleColorStderr, err: errors.New("exit 1")}
	}
	r := &scriptedRunner{responses: resps}
	g := &Generator{Runner: r, MaxTries: 5}
	err := g.Generate(context.Background(), Input{
		VideoPath:       "/in.mp4",
		OutputPath:      filepath.Join(t.TempDir(), "out.jpg"),
		DurationSeconds: 50,
	})
	if !errors.Is(err, ErrAllTriesSingleColor) {
		t.Errorf("err=%v, want ErrAllTriesSingleColor", err)
	}
	if len(r.calls) != 5 {
		t.Errorf("calls=%d, want 5", len(r.calls))
	}
}

func TestGenerate_NonSingleColorErrorSurfacesImmediately(t *testing.T) {
	// Retrying arbitrary ffmpeg errors would mask broken input or invocation.
	r := &scriptedRunner{
		responses: []response{
			{stderr: "Invalid data found when processing input", err: errors.New("exit 1")},
		},
	}
	g := &Generator{Runner: r}
	err := g.Generate(context.Background(), Input{
		VideoPath:       "/in.mp4",
		OutputPath:      filepath.Join(t.TempDir(), "out.jpg"),
		DurationSeconds: 100,
	})
	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, ErrAllTriesSingleColor) {
		t.Error("non-monochrome error misclassified as single-color")
	}
	if !strings.Contains(err.Error(), "Invalid data") {
		t.Errorf("err=%v, want stderr excerpt", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("calls=%d, want 1 (no retry on non-single-color)", len(r.calls))
	}
}

func TestGenerate_CtxCancelPassesThrough(t *testing.T) {
	r := &scriptedRunner{
		responses: []response{{err: context.Canceled}},
	}
	g := &Generator{Runner: r}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := g.Generate(ctx, Input{
		VideoPath:       "/in.mp4",
		OutputPath:      filepath.Join(t.TempDir(), "out.jpg"),
		DurationSeconds: 100,
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v, want context.Canceled", err)
	}
}

func TestInitialOffset(t *testing.T) {
	cases := []struct {
		duration float64
		want     float64
	}{
		{0, 5},
		{10, 5},
		{100, 10},
		{3000, 300},
		{10000, 600},
		{-10, 5},
	}
	for _, c := range cases {
		got := initialOffset(c.duration)
		if got != c.want {
			t.Errorf("initialOffset(%v)=%v, want %v", c.duration, got, c.want)
		}
	}
}

func TestFFmpegArgs_Shape(t *testing.T) {
	got := ffmpegArgs(42.5, Input{VideoPath: "/in.mp4", OutputPath: "/out.jpg"})
	want := []string{
		"-y",
		"-ss", "42.50",
		"-i", "/in.mp4",
		"-vframes", "1",
		"-q:v", "3",
		"/out.jpg",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args=%v\nwant=%v", got, want)
	}
}

func TestGenerate_DefaultBudgetAndBump(t *testing.T) {
	resps := make([]response, 5)
	for i := range resps {
		resps[i] = response{stderr: singleColorStderr, err: errors.New("exit 1")}
	}
	r := &scriptedRunner{responses: resps}
	g := &Generator{Runner: r}
	_ = g.Generate(context.Background(), Input{
		VideoPath:  "/in.mp4",
		OutputPath: filepath.Join(t.TempDir(), "out.jpg"),
	})
	if len(r.calls) != 5 {
		t.Errorf("calls=%d, want 5", len(r.calls))
	}
}

func TestGenerate_DeadlineExceededPassesThrough(t *testing.T) {
	r := &scriptedRunner{
		responses: []response{{err: context.DeadlineExceeded}},
	}
	g := &Generator{Runner: r}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := g.Generate(ctx, Input{
		VideoPath:       "/in.mp4",
		OutputPath:      filepath.Join(t.TempDir(), "out.jpg"),
		DurationSeconds: 100,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err=%v, want DeadlineExceeded", err)
	}
}
