package remux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
)

// DefaultFFmpegPath uses the ffmpeg executable found through PATH.
const DefaultFFmpegPath = "ffmpeg"

const partSuffix = ".part"

// Runner executes an external command and returns after it exits.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stderr io.Writer) error
}

// Remuxer stream-copies prepared input into MP4 or M4A containers.
// It can be shared across jobs when its Runner supports concurrent calls.
type Remuxer struct {
	// FFmpegPath defaults to DefaultFFmpegPath.
	FFmpegPath string

	// Runner defaults to synchronous subprocess execution.
	Runner Runner

	// Log discards failure details when nil.
	Log *slog.Logger
}

// RunInput describes one remux operation and its scratch file owner.
type RunInput struct {
	Mode Mode

	Kind Kind

	// InputPath names the description returned by PrepareInput.
	InputPath string

	// OutputDir must already exist.
	OutputDir string

	// OutputBasename excludes the container extension.
	OutputBasename string

	// Faststart moves the MP4 index before media bytes for progressive playback.
	Faststart bool

	// Files must own OutputDir when scratch accounting is required; nil uses local files.
	Files FileOperations
}

// OutputPath returns the committed container path.
func (in RunInput) OutputPath() string {
	return filepath.Join(in.OutputDir, in.OutputBasename+in.Kind.OutputExt())
}

// Run stream-copies input and commits output through Files after ffmpeg succeeds.
// Failure or cancellation removes partial output; cleanup errors are joined with the failure.
// Command failures include up to 8 KiB of stderr.
func (r *Remuxer) Run(ctx context.Context, in RunInput) (err error) {
	log := r.logOrDiscard()
	runner := r.runnerOrExec()
	bin := r.binOrDefault()
	files := filesOrLocal(in.Files)

	finalPath := in.OutputPath()
	partPath := finalPath + partSuffix

	args, err := ffmpegArgs(in, partPath)
	if err != nil {
		return err
	}
	if err := removePartial(files, partPath); err != nil {
		return fmt.Errorf("remux: remove stale partial output: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if cleanupErr := removePartial(files, partPath); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remux: remove partial output: %w", cleanupErr))
			}
		}
	}()

	var stderr bytes.Buffer
	runErr := runner.Run(ctx, bin, args, &stderr)
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return runErr
		}
		preview := truncate(stderr.String(), 8<<10)
		log.Warn("ffmpeg failed",
			"input", in.InputPath,
			"output", finalPath,
			"stderr", preview)
		return fmt.Errorf("remux: ffmpeg failed: %w\nstderr:\n%s", runErr, preview)
	}

	if err := files.Rename(partPath, finalPath); err != nil {
		return fmt.Errorf("remux: commit rename %s → %s: %w", partPath, finalPath, err)
	}
	committed = true
	return nil
}

// ffmpegArgs selects input parsing and stream-copy options.
// Output needs an explicit MP4 muxer because .part has no recognized container extension.
func ffmpegArgs(in RunInput, outputPath string) ([]string, error) {
	var args []string
	switch in.Mode {
	case ModeTS:
		args = []string{
			"-y",
			"-f", "concat",
			"-safe", "0",
			"-i", in.InputPath,
			"-c", "copy",
		}
	case ModeFMP4:
		args = []string{
			"-y",
			"-i", in.InputPath,
			"-c", "copy",
		}
	default:
		return nil, fmt.Errorf("remux: unknown mode %q", in.Mode)
	}
	if in.Faststart {
		args = append(args, "-movflags", "+faststart")
	}
	return append(args, "-f", "mp4", outputPath), nil
}

func (r *Remuxer) logOrDiscard() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.New(slog.DiscardHandler)
}

func (r *Remuxer) runnerOrExec() Runner {
	if r.Runner != nil {
		return r.Runner
	}
	return execRunner{}
}

func (r *Remuxer) binOrDefault() string {
	if r.FFmpegPath != "" {
		return r.FFmpegPath
	}
	return DefaultFFmpegPath
}

// truncate limits s to n bytes and appends a truncation marker.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	// Killing ffmpeg is safe because Run or Heal removes uncommitted output.
	return cmd.Run()
}
