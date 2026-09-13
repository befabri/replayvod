// Package thumbnail captures JPEG thumbnails and preview strips with ffmpeg
// and periodically fetches live snapshots.
package thumbnail

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

// DefaultFFmpegPath is resolved through PATH when FFmpegPath is empty.
const DefaultFFmpegPath = "ffmpeg"

// Retry detection relies on ffmpeg's stderr wording for monochrome frames.
const singleColorMarker = "Image is a single color"

// Runner executes a command and waits for it to exit.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stderr io.Writer) error
}

// FileOperations coordinates output replacement with scratch accounting.
type FileOperations interface {
	Remove(string) error
}

// Generator captures thumbnails and preview strips; its zero value uses ffmpeg.
// Sharing a Generator requires a Runner that supports concurrent calls.
type Generator struct {
	FFmpegPath string
	Runner     Runner
	Log        *slog.Logger

	// MaxTries defaults to 5 when nonpositive.
	MaxTries int

	// BumpSeconds advances the capture offset on retries; nonpositive values use 60.
	BumpSeconds float64
}

// Input specifies a single-frame JPEG capture.
type Input struct {
	// Files owns output removal; nil uses the local filesystem.
	Files FileOperations

	VideoPath string

	// OutputPath names a JPEG file replaced before each attempt.
	OutputPath string

	// DurationSeconds sets the initial offset to 10% of the duration, clamped
	// to [5, 600] seconds; nonpositive values use a 5-second offset.
	DurationSeconds float64
}

// Generate captures a JPEG, retrying only reported monochrome frames.
// It returns ErrAllTriesSingleColor when every attempt reports that failure.
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
		if err := resetOutput(ctx, in.OutputPath, in.Files); err != nil {
			return err
		}
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

// ErrAllTriesSingleColor means every capture attempt reported a monochrome frame.
var ErrAllTriesSingleColor = errors.New("thumbnail: all retries returned single-color frames")

// StripInput specifies a JPEG preview grid sampled across a video.
type StripInput struct {
	// Files owns output removal; nil uses the local filesystem.
	Files FileOperations

	VideoPath string

	// OutputPath names a JPEG file replaced before generation.
	OutputPath string

	// DurationSeconds must be positive and determines the sample rate.
	DurationSeconds float64

	// Frames, Columns, FrameWidth, and Quality use 12, 4, 160, and 3 when
	// nonpositive; FrameWidth is in pixels and Quality is ffmpeg's JPEG scale.
	Frames     int
	Columns    int
	FrameWidth int
	Quality    int
}

// GenerateStrip captures a preview grid without retrying monochrome frames.
// A nonpositive duration fails before changing the output.
func (g *Generator) GenerateStrip(ctx context.Context, in StripInput) error {
	if in.DurationSeconds <= 0 {
		return fmt.Errorf("thumbnail strip: non-positive duration %v", in.DurationSeconds)
	}
	frames := in.Frames
	if frames <= 0 {
		frames = 12
	}
	cols := in.Columns
	if cols <= 0 {
		cols = 4
	}
	rows := (frames + cols - 1) / cols
	fw := in.FrameWidth
	if fw <= 0 {
		fw = 160
	}
	q := in.Quality
	if q <= 0 {
		q = 3
	}
	runner := g.Runner
	if runner == nil {
		runner = execRunner{}
	}
	bin := g.FFmpegPath
	if bin == "" {
		bin = DefaultFFmpegPath
	}

	// ffmpeg accepts a ratio, avoiding sample-rate rounding on short clips.
	filter := fmt.Sprintf("fps=%d/%.6f,scale=%d:-1,tile=%dx%d",
		frames, in.DurationSeconds, fw, cols, rows)

	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", in.VideoPath,
		"-vf", filter,
		"-frames:v", "1",
		"-q:v", fmt.Sprintf("%d", q),
		"-f", "image2",
		in.OutputPath,
	}
	var stderr bytes.Buffer
	if err := resetOutput(ctx, in.OutputPath, in.Files); err != nil {
		return err
	}
	if err := runner.Run(ctx, bin, args, &stderr); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("thumbnail strip: ffmpeg failed: %w\nstderr:\n%s",
			err, truncate(stderr.String(), 4<<10))
	}
	return nil
}

func resetOutput(ctx context.Context, path string, files FileOperations) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remove := os.Remove
	if files != nil {
		remove = files.Remove
	}
	// ffmpeg's -y can shrink existing output, invalidating cached scratch usage.
	if err := remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("thumbnail: remove previous output: %w", err)
	}
	return nil
}

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

// Putting -ss before -i uses fast demuxer seeking; -q:v uses a JPEG scale
// where higher values reduce quality.
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

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	return cmd.Run()
}
