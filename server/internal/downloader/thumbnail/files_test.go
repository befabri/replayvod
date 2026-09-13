package thumbnail

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/replayvod/server/internal/mediastore"
)

type fileRunner func(context.Context, string, []string, io.Writer) error

func (r fileRunner) Run(ctx context.Context, binary string, args []string, stderr io.Writer) error {
	return r(ctx, binary, args, stderr)
}

func thumbnailWorkspace(t *testing.T) *mediastore.Workspace {
	t.Helper()
	w, err := mediastore.NewScratch(t.TempDir()).New("thumbnail", 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(true); err != nil {
			t.Error(err)
		}
	})
	return w
}

func writeOutput(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func generateOutput(ctx context.Context, g *Generator, path string, files FileOperations, strip bool) error {
	if strip {
		return g.GenerateStrip(ctx, StripInput{OutputPath: path, DurationSeconds: 120, Files: files})
	}
	return g.Generate(ctx, Input{OutputPath: path, DurationSeconds: 120, Files: files})
}

func TestGenerateRemovesRecoveredAndRetriedOutput(t *testing.T) {
	for _, strip := range []bool{false, true} {
		t.Run(map[bool]string{false: "thumbnail", true: "strip"}[strip], func(t *testing.T) {
			w := thumbnailWorkspace(t)
			path := filepath.Join(w.Dir, "output.jpg")
			writeOutput(t, path, "interrupted previous thumbnail")
			if err := w.Reserve(1 << 16); err != nil {
				t.Fatal(err)
			}
			calls := 0
			g := &Generator{Runner: fileRunner(func(_ context.Context, _ string, args []string, stderr io.Writer) error {
				calls++
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("ffmpeg would truncate existing output on attempt %d: %v", calls, err)
				}
				writeOutput(t, args[len(args)-1], "new thumbnail")
				if err := w.Reserve(1 << 16); err != nil {
					t.Fatal(err)
				}
				if !strip && calls == 1 {
					_, _ = io.WriteString(stderr, singleColorStderr)
					return errors.New("single color")
				}
				return nil
			})}
			if err := generateOutput(t.Context(), g, path, w, strip); err != nil {
				t.Fatal(err)
			}
			wantCalls := 2
			if strip {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("ffmpeg calls=%d, want %d", calls, wantCalls)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "new thumbnail" {
				t.Fatalf("thumbnail=%q err=%v", data, err)
			}
		})
	}
}

func TestGenerateRequiresOutputOwnership(t *testing.T) {
	for _, strip := range []bool{false, true} {
		for _, refusal := range []string{"closed workspace", "outside workspace", "canceled"} {
			t.Run(map[bool]string{false: "thumbnail/", true: "strip/"}[strip]+refusal, func(t *testing.T) {
				w := thumbnailWorkspace(t)
				path := filepath.Join(w.Dir, "output.jpg")
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				switch refusal {
				case "closed workspace":
					if err := w.Close(false); err != nil {
						t.Fatal(err)
					}
				case "outside workspace":
					path = filepath.Join(t.TempDir(), "output.jpg")
				case "canceled":
					cancel()
				}
				writeOutput(t, path, "preserved thumbnail")
				calls := 0
				g := &Generator{Runner: fileRunner(func(_ context.Context, _ string, args []string, _ io.Writer) error {
					calls++
					writeOutput(t, args[len(args)-1], "overwritten thumbnail")
					return nil
				})}
				err := generateOutput(ctx, g, path, w, strip)
				if err == nil || calls != 0 {
					t.Fatalf("refused mutation invoked ffmpeg: calls=%d err=%v", calls, err)
				}
				if refusal == "canceled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation cause was lost: %v", err)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "preserved thumbnail" {
					t.Fatalf("refused mutation changed output: %q err=%v", data, err)
				}
			})
		}
	}
}
