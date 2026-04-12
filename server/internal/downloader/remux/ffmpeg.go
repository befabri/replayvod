package remux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// DefaultFFmpegPath is the binary the driver invokes when
// Remuxer.FFmpegPath is empty. "ffmpeg" resolves via the usual
// PATH lookup — operators who need a specific binary (e.g. a
// custom build with HEVC support) can override at construction.
const DefaultFFmpegPath = "ffmpeg"

// Runner abstracts os/exec so tests can substitute a mock
// without shelling out to a real ffmpeg. The real implementation
// is execRunner below.
//
// Kept as a narrow interface (single method) rather than embedding
// exec.Cmd so tests don't have to satisfy the full Cmd surface.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stderr io.Writer) error
}

// Remuxer drives ffmpeg to turn a prepared input description
// (segments.txt for TS, media.m3u8 for fMP4) into a single
// stream-copied MP4/M4A. One Remuxer is safe to share across
// jobs — the type holds no per-job state.
type Remuxer struct {
	// FFmpegPath overrides DefaultFFmpegPath. Empty = default.
	FFmpegPath string

	// Runner lets tests substitute a mock. Nil uses execRunner.
	Runner Runner

	// Log is where ffmpeg's stderr preview is written on
	// failure. Nil logs to discard.
	Log *slog.Logger
}

// RunInput parameterizes a single remux invocation. The two
// input formats require different ffmpeg flag shapes, which is
// why this struct exists rather than a positional-arg API.
type RunInput struct {
	// Mode distinguishes the concat demuxer path (TS) from the
	// standalone-playlist path (fMP4).
	Mode Mode

	// Kind picks the output extension (OutputFile overrides if
	// set, but typical callers leave OutputFile empty and let
	// this field drive both the extension and the ffmpeg-level
	// behavior).
	Kind Kind

	// InputPath is the segments.txt (TS) or media.m3u8 (fMP4)
	// written by PrepareInput.
	InputPath string

	// OutputDir is the directory where the remuxed file lands.
	// Combined with OutputBasename + Kind to produce the final
	// path. Must already exist.
	OutputDir string

	// OutputBasename is the filename without extension — the
	// orchestrator's `<base>-part<NN>` form. Extension is
	// appended from Kind.
	OutputBasename string
}

// OutputPath computes the final file path ffmpeg writes to.
// Exposed so callers can log the path before invocation.
func (in RunInput) OutputPath() string {
	return strings.TrimSuffix(in.OutputDir, "/") + "/" + in.OutputBasename + in.Kind.OutputExt()
}

// Run invokes ffmpeg with the flags appropriate to Mode + Kind
// and streams stderr to an in-memory buffer. On non-zero exit,
// the buffer's contents — truncated to 8 KB so an ffmpeg flood
// doesn't balloon the error message — are included in the
// returned error for diagnostic purposes.
//
// The context must be honored by the Runner. execRunner does
// this via exec.CommandContext + CommandContext's default SIGKILL
// on cancel; mock runners in tests should check ctx themselves.
func (r *Remuxer) Run(ctx context.Context, in RunInput) error {
	log := r.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	runner := r.Runner
	if runner == nil {
		runner = execRunner{}
	}
	bin := r.FFmpegPath
	if bin == "" {
		bin = DefaultFFmpegPath
	}

	args, err := ffmpegArgs(in)
	if err != nil {
		return err
	}

	var stderr bytes.Buffer
	runErr := runner.Run(ctx, bin, args, &stderr)
	if runErr == nil {
		return nil
	}

	// Cancellation wins: don't dress up ctx.Err() with
	// ffmpeg stderr that the operator already knows is
	// meaningless.
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return runErr
	}

	preview := truncate(stderr.String(), 8<<10)
	log.Warn("ffmpeg failed",
		"input", in.InputPath,
		"output", in.OutputPath(),
		"stderr", preview)
	return fmt.Errorf("remux: ffmpeg failed: %w\nstderr:\n%s", runErr, preview)
}

// ffmpegArgs returns the argv slice for Mode. Kept separate from
// Run so tests can assert exact argument shape without running
// the command.
//
// Flag reference:
//   -y: overwrite output without prompting
//   -f concat: use the concat demuxer (TS path)
//   -safe 0: allow absolute paths in concat input
//   -i: input path
//   -c copy: stream-copy all streams (no re-encode)
func ffmpegArgs(in RunInput) ([]string, error) {
	switch in.Mode {
	case ModeTS:
		return []string{
			"-y",
			"-f", "concat",
			"-safe", "0",
			"-i", in.InputPath,
			"-c", "copy",
			in.OutputPath(),
		}, nil
	case ModeFMP4:
		return []string{
			"-y",
			"-i", in.InputPath,
			"-c", "copy",
			in.OutputPath(),
		}, nil
	default:
		return nil, fmt.Errorf("remux: unknown mode %q", in.Mode)
	}
}

// truncate returns s clipped to n bytes. Keeps the trailing
// indicator when truncation happens so log readers don't see an
// arbitrarily chopped buffer.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}

// execRunner is the production Runner: os/exec with stderr piped
// to the caller's buffer and stdout discarded. ffmpeg writes
// progress + errors to stderr exclusively.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	// CommandContext's default is to SIGKILL on cancel. That's
	// aggressive but matches the scratch-file lifecycle: if
	// ffmpeg is canceled, its partial output is the next
	// orphan the startup sweep will clear, so we don't need
	// graceful SIGTERM.
	_ = os.Stdout // keep import graph stable for future orchestration hooks
	return cmd.Run()
}
