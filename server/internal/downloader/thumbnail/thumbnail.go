// Package thumbnail captures a single JPEG frame from a remuxed
// video using ffmpeg. Invoked at Stage 8 of the pipeline for
// video jobs; audio jobs skip thumbnail generation entirely
// (the UI falls back to the channel avatar).
//
// Port of v1's thumbnail retry loop: when ffmpeg's single-frame
// output matches "Image is a single color" — which happens
// against Twitch's "starting soon" slate and some partial-frame
// edge cases — bump the capture timestamp +60s and try again,
// up to MaxTries total. Without the retry a short recording
// that starts on a slate would get a monochrome thumbnail and
// the UI would show a blank tile.
package thumbnail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
)

// DefaultFFmpegPath is the binary invoked when FFmpegPath is
// empty. PATH lookup via os/exec.
const DefaultFFmpegPath = "ffmpeg"

// singleColorMarker is the substring ffmpeg emits when the
// picked frame is a single solid color. Matching on the string
// is fragile across ffmpeg versions, but every version from
// v1's deployment through current has used this phrase, and
// the cost of a false negative (one ugly thumbnail) is low.
const singleColorMarker = "Image is a single color"

// Runner abstracts os/exec so tests can verify retry behavior
// without invoking ffmpeg. Matches the sibling remux/probe
// Runner interface shape; kept local rather than sharing to
// avoid a cross-package dep for one method.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stderr io.Writer) error
}

// Generator captures a single-frame thumbnail. Shared across
// jobs; holds no per-file state. Zero values for MaxTries /
// BumpSeconds pick sensible defaults from v1.
type Generator struct {
	FFmpegPath string
	Runner     Runner
	Log        *slog.Logger

	// MaxTries bounds the single-color-retry budget. Default 5.
	// Each retry advances the capture offset by BumpSeconds,
	// so MaxTries=5 + BumpSeconds=60 walks the first 5 minutes
	// of a stream looking for a non-monochrome frame.
	MaxTries int

	// BumpSeconds is the offset added on each retry. Default 60.
	BumpSeconds float64
}

// Input parameterizes one Generate call. Kept as a struct so
// adding fields (watermark, dimensions, etc.) later doesn't
// break callers.
type Input struct {
	// VideoPath is the input file — typically the remuxed
	// .mp4 from the remux step.
	VideoPath string

	// OutputPath is the .jpg we write. Orchestrator decides
	// the naming; this package doesn't assume anything.
	OutputPath string

	// DurationSeconds is the video's total length, used to
	// pick the initial offset (10% clamped to [5, 600]). Pass
	// probe.Result.Duration. When 0, the Generator falls back
	// to a fixed 5s offset so operators who skip probing (or
	// probe failed) still get a thumbnail.
	DurationSeconds float64
}

// Generate runs the retry loop. Returns ErrAllTriesSingleColor
// if every attempt produced a monochrome frame — the caller can
// then decide to leave the thumbnail unset (spec Stage 8) rather
// than ship a blank tile.
//
// Other errors (ffmpeg invocation failure, ctx cancel, unrecog-
// nized stderr) surface directly; the retry loop only fires on
// the specific single-color case.
func (g *Generator) Generate(ctx context.Context, in Input) error {
	log := g.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	runner := g.Runner
	if runner == nil {
		runner = execRunner{}
	}
	bin := g.FFmpegPath
	if bin == "" {
		bin = DefaultFFmpegPath
	}
	maxTries := g.MaxTries
	if maxTries <= 0 {
		maxTries = 5
	}
	bump := g.BumpSeconds
	if bump <= 0 {
		bump = 60
	}

	offset := initialOffset(in.DurationSeconds)

	for attempt := 0; attempt < maxTries; attempt++ {
		var stderr bytes.Buffer
		err := runner.Run(ctx, bin, ffmpegArgs(offset, in), &stderr)
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		stderrStr := stderr.String()
		if !strings.Contains(stderrStr, singleColorMarker) {
			// Unknown ffmpeg failure — surface immediately
			// with the stderr preview so operator logs tell
			// the operator what actually broke.
			return fmt.Errorf("thumbnail: ffmpeg failed at offset %.1fs: %w\nstderr:\n%s",
				offset, err, truncate(stderrStr, 4<<10))
		}
		log.Debug("thumbnail monochrome; bumping offset",
			"attempt", attempt+1,
			"offset", offset)
		offset += bump
	}
	return ErrAllTriesSingleColor
}

// ErrAllTriesSingleColor surfaces when the retry budget is
// exhausted without capturing a non-monochrome frame. Callers
// typically leave the thumbnail unset on this — the UI's
// fallback (channel avatar) is more useful than a blank JPEG.
var ErrAllTriesSingleColor = errors.New("thumbnail: all retries returned single-color frames")

// initialOffset picks the first capture timestamp: 10% of
// duration, clamped to [5, 600]. v1 heuristic retained — a
// 30-minute stream pulls from ~3 minutes in, a 3-hour stream
// pulls from the 10-minute cap.
//
// Fallback to 5s when duration is 0 (probe skipped or failed) —
// most streams have opened their intro by then even if they
// haven't started the main content.
func initialOffset(duration float64) float64 {
	if duration <= 0 {
		return 5
	}
	off := duration * 0.10
	if off < 5 {
		return 5
	}
	if off > 600 {
		return 600
	}
	return off
}

// ffmpegArgs returns the argv for a single-frame JPEG capture.
// -ss before -i is the fast-seek path (demux keyframe lookup)
// rather than the slow decode-seek; the small quality cost is
// acceptable for thumbnail use.
//
// -q:v 3 is VBR quality scale 3/31 (31 worst); 3 gives a
// ~100 KB JPEG for a 1080p frame, which is what the UI expects.
func ffmpegArgs(offsetSec float64, in Input) []string {
	return []string{
		"-y",
		"-ss", fmt.Sprintf("%.2f", offsetSec),
		"-i", in.VideoPath,
		"-vframes", "1",
		"-q:v", "3",
		in.OutputPath,
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}

// execRunner is the production Runner.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	return cmd.Run()
}
