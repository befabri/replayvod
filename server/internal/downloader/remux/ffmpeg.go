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
	"path/filepath"
)

// DefaultFFmpegPath is the binary the driver invokes when
// Remuxer.FFmpegPath is empty. "ffmpeg" resolves via the usual
// PATH lookup — operators who need a specific binary (e.g. a
// custom build with HEVC support) can override at construction.
const DefaultFFmpegPath = "ffmpeg"

// partSuffix is the .part extension we append to ffmpeg's output
// while it's in flight. Matches the hls.PartWriter pattern so
// the startup sweep and the mid-crash cleanup story look the
// same across the pipeline.
const partSuffix = ".part"

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

	// Kind picks the output extension.
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

// OutputPath computes the final file path ffmpeg will land at
// on successful Run. Exposed so callers can log the path before
// invocation.
func (in RunInput) OutputPath() string {
	return filepath.Join(in.OutputDir, in.OutputBasename+in.Kind.OutputExt())
}

// Run invokes ffmpeg and produces a committed output file at
// OutputPath. ffmpeg actually writes to OutputPath+".part"; on
// a clean exit we atomic-rename to the final name, and on any
// failure or cancellation we remove the partial file. Matches
// the hls.PartWriter lifecycle so the startup sweep can treat
// stray .part files uniformly across the pipeline.
//
// On non-zero exit, stderr is captured to an 8 KiB preview and
// included in the error message for diagnostics. Cancellation
// surfaces as the raw ctx error without the stderr dressing —
// the operator already knows why they canceled.
func (r *Remuxer) Run(ctx context.Context, in RunInput) error {
	log := r.logOrDiscard()
	runner := r.runnerOrExec()
	bin := r.binOrDefault()

	finalPath := in.OutputPath()
	partPath := finalPath + partSuffix

	args, err := ffmpegArgs(in, partPath)
	if err != nil {
		return err
	}

	// Cleanup on every non-success path — including a panic
	// between runner.Run and the rename. The committed flag
	// gates the removal so the happy path leaves the final
	// file intact. os.Remove on a non-existent path is a no-op
	// so failed-ffmpeg + already-gone-.part paths are fine too.
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(partPath)
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

	// Success: rename .part → final. A rename failure here is
	// rare (cross-filesystem, disk full, permissions) but real;
	// the deferred Remove cleans up the orphan .part and we
	// surface the error.
	if err := os.Rename(partPath, finalPath); err != nil {
		return fmt.Errorf("remux: commit rename %s → %s: %w", partPath, finalPath, err)
	}
	committed = true
	return nil
}

// ffmpegArgs returns the argv slice for Mode, parameterized by
// the exact output path ffmpeg should write to. Run passes
// OutputPath()+".part" so the caller can rename-commit on
// success; tests pass whatever they want to assert.
//
// Flag reference:
//   -y: overwrite output without prompting
//   -f concat: input demuxer for the TS path
//   -safe 0: allow absolute paths in concat input
//   -i: input path
//   -c copy: stream-copy all streams (no re-encode)
//   -f mp4 (output side): force the output muxer. Required because
//     the output path ends in `.part` (our atomic-rename convention)
//     and ffmpeg 8.1 refuses to auto-detect muxer from that
//     extension with "Unable to choose an output format for ...;
//     use a standard extension for the filename or specify the
//     format manually." Older ffmpeg versions happily guessed mp4
//     from the double extension, which hid this assumption. Audio
//     (.m4a) output is also the mp4 muxer — m4a is just an mp4
//     container holding only audio, so one value covers both kinds.
func ffmpegArgs(in RunInput, outputPath string) ([]string, error) {
	switch in.Mode {
	case ModeTS:
		return []string{
			"-y",
			"-f", "concat",
			"-safe", "0",
			"-i", in.InputPath,
			"-c", "copy",
			"-f", "mp4",
			outputPath,
		}, nil
	case ModeFMP4:
		return []string{
			"-y",
			"-i", in.InputPath,
			"-c", "copy",
			"-f", "mp4",
			outputPath,
		}, nil
	default:
		return nil, fmt.Errorf("remux: unknown mode %q", in.Mode)
	}
}

// Internal shortcut helpers so Run and Heal don't repeat the
// same nil-check boilerplate five ways.
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
	// CommandContext sends SIGKILL on cancel. Run wraps output
	// in a .part file and deletes it on non-success, so the
	// aggressive signal is fine — no partial final file gets
	// left around for the startup sweep to guess at.
	return cmd.Run()
}
