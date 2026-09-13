package remux

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/mediastore"
)

type workspaceFiles struct {
	*mediastore.Workspace
	removes, renames, writes int
	truncateSizes            []int64
	removeFailure            int
	removeErr, renameErr     error
}

func (f *workspaceFiles) Remove(path string) error {
	f.removes++
	if f.removes == f.removeFailure {
		return f.removeErr
	}
	return f.Workspace.Remove(path)
}

func (f *workspaceFiles) Rename(oldPath, newPath string) error {
	f.renames++
	if f.renameErr != nil {
		return f.renameErr
	}
	return f.Workspace.Rename(oldPath, newPath)
}

func (f *workspaceFiles) Truncate(file *os.File, size int64) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	f.truncateSizes = append(f.truncateSizes, info.Size())
	return f.Workspace.Truncate(file, size)
}

func (f *workspaceFiles) WriteFile(ctx context.Context, file *os.File, data []byte) (int, error) {
	f.writes++
	return f.Workspace.WriteFile(ctx, file, data)
}

func newWorkspaceFiles(t *testing.T) *workspaceFiles {
	t.Helper()
	w, err := mediastore.NewScratch(t.TempDir()).New("remux", 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close(true) })
	return &workspaceFiles{Workspace: w}
}

type fileRunner func(context.Context, string, []string, io.Writer) error

func (f fileRunner) Run(ctx context.Context, binary string, args []string, stderr io.Writer) error {
	return f(ctx, binary, args, stderr)
}

func TestRemuxOwnsOutputReplacementAndCleanup(t *testing.T) {
	for _, heal := range []bool{false, true} {
		for _, outcome := range []string{"success", "failure", "panic", "rename failure"} {
			t.Run(outcome+map[bool]string{false: "/run", true: "/heal"}[heal], func(t *testing.T) {
				files := newWorkspaceFiles(t)
				final := filepath.Join(files.Dir, "output.mp4")
				partial := final + partSuffix
				writeFile(t, final, "previous output")
				writeFile(t, partial, "interrupted output")
				failure := errors.New("ffmpeg failed")
				if outcome == "rename failure" {
					files.renameErr = failure
				}
				r := &Remuxer{Runner: fileRunner(func(_ context.Context, _ string, args []string, _ io.Writer) error {
					if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) || files.removes != 1 {
						t.Fatalf("ffmpeg would overwrite an unaccounted partial: stat=%v removes=%d", err, files.removes)
					}
					writeFile(t, args[len(args)-1], "replacement")
					switch outcome {
					case "failure":
						return failure
					case "panic":
						panic(failure)
					}
					return nil
				})}
				var err error
				var recovered any
				func() {
					defer func() { recovered = recover() }()
					if heal {
						err = r.Heal(t.Context(), "input.mp4", final, KindVideo, files)
					} else {
						err = r.Run(t.Context(), RunInput{Mode: ModeTS, Kind: KindVideo, InputPath: "segments.txt", OutputDir: files.Dir, OutputBasename: "output", Files: files})
					}
				}()
				want, removes, renames := "previous output", 2, 0
				switch outcome {
				case "success":
					want, removes, renames = "replacement", 1, 1
					if err != nil || recovered != nil {
						t.Fatalf("successful remux failed: %v panic=%v", err, recovered)
					}
				case "panic":
					if recovered != failure {
						t.Fatalf("runner panic was lost: %v", recovered)
					}
				default:
					if !errors.Is(err, failure) || recovered != nil {
						t.Fatalf("runner failure was lost: %v panic=%v", err, recovered)
					}
					if outcome == "rename failure" {
						renames = 1
					}
				}
				if data, err := os.ReadFile(final); err != nil || string(data) != want {
					t.Fatalf("final output=%q err=%v, want %q", data, err, want)
				}
				if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("partial output survived settlement: %v", err)
				}
				if files.removes != removes || files.renames != renames {
					t.Fatalf("workspace mutations: remove=%d rename=%d, want %d/%d", files.removes, files.renames, removes, renames)
				}
			})
		}
	}
}

func TestRemuxReportsCleanupFailureAndDoesNotOverwriteBlockedPartial(t *testing.T) {
	for _, heal := range []bool{false, true} {
		for _, failAt := range []int{1, 2} {
			files := newWorkspaceFiles(t)
			files.removeFailure, files.removeErr = failAt, errors.New("scratch removal denied")
			final := filepath.Join(files.Dir, "output.mp4")
			writeFile(t, final+partSuffix, "interrupted output")
			failure := errors.New("ffmpeg failed")
			calls := 0
			r := &Remuxer{Runner: fileRunner(func(_ context.Context, _ string, args []string, _ io.Writer) error {
				calls++
				writeFile(t, args[len(args)-1], "failed output")
				return failure
			})}
			var err error
			if heal {
				err = r.Heal(t.Context(), "input.mp4", final, KindVideo, files)
			} else {
				err = r.Run(t.Context(), RunInput{Mode: ModeTS, OutputDir: files.Dir, OutputBasename: "output", Files: files})
			}
			if !errors.Is(err, files.removeErr) || calls != failAt-1 || errors.Is(err, failure) != (failAt == 2) {
				t.Fatalf("heal=%t remove failure=%d: calls=%d error=%v", heal, failAt, calls, err)
			}
		}
	}
}

func TestPrepareInputAccountsForReplacingExistingDescriptions(t *testing.T) {
	for _, mode := range []Mode{ModeTS, ModeFMP4} {
		t.Run(string(mode), func(t *testing.T) {
			files := newWorkspaceFiles(t)
			name := "segments.txt"
			if mode == ModeFMP4 {
				name = "media.m3u8"
				writeFile(t, filepath.Join(files.Dir, "init.mp4"), "init")
				writeFile(t, filepath.Join(files.Dir, "1.m4s"), "segment")
			} else {
				writeFile(t, filepath.Join(files.Dir, "1.ts"), "segment")
			}
			previous := strings.Repeat("obsolete input\n", 1024)
			writeFile(t, filepath.Join(files.Dir, name), previous)
			path, err := PrepareInput(t.Context(), files.Dir, mode, files)
			if err != nil {
				t.Fatal(err)
			}
			if files.writes != 1 || len(files.truncateSizes) != 1 || files.truncateSizes[0] != int64(len(previous)) {
				t.Fatalf("input overwritten outside workspace: writes=%d prior sizes=%v", files.writes, files.truncateSizes)
			}
			data, err := os.ReadFile(path)
			if err != nil || strings.Contains(string(data), "obsolete") || len(data) == 0 {
				t.Fatalf("replacement description=%q err=%v", data, err)
			}
		})
	}
}
