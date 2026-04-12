// Package probe wraps ffprobe for duration + stream + size
// extraction after the remux step.
//
// The orchestrator consumes Result in two places:
//   - Stage 7 (metadata): Duration + Size land on the
//     video_parts row.
//   - Stage 9 (corruption check): VideoStream.Duration and
//     AudioStream.Duration compared against Duration. A >50s
//     mismatch triggers a remux heal pass.
//
// ffprobe's JSON output is verbose; we decode into a narrow
// shape rather than dragging in the full schema. Missing fields
// are tolerated — some remuxed files omit stream-level durations
// and that's a legitimate "couldn't measure" signal, not a parse
// error.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
)

// DefaultFFprobePath is the binary invoked when Probe.FFprobePath
// is empty. PATH lookup via os/exec.
const DefaultFFprobePath = "ffprobe"

// Runner abstracts os/exec so tests can drive Probe without
// shelling out. Same interface shape as remux.Runner — duplicated
// here rather than imported because the two packages are siblings
// and the indirection is one method.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error
}

// Probe runs ffprobe against a file. One Probe is shared across
// jobs; it holds no per-file state.
type Probe struct {
	FFprobePath string
	Runner      Runner
	Log         *slog.Logger
}

// Result is the decoded ffprobe output plus the file size we
// stat'd alongside. All duration fields are seconds; Size is
// bytes. Zero values mean "not reported by ffprobe" — the
// orchestrator's corruption check treats missing stream
// duration as "can't compare; skip healing."
type Result struct {
	// Duration is format.duration — ffprobe's top-level view
	// of the container's total length.
	Duration float64

	// VideoStream is the first video stream encountered, nil
	// when the file is audio-only.
	VideoStream *Stream

	// AudioStream is the first audio stream encountered, nil
	// when the file is video-only (unusual but possible).
	AudioStream *Stream

	// Size is the os.Stat'd file size in bytes. Not from
	// ffprobe — ffprobe's format.size is the declared size
	// which can lag the actual file for just-remuxed outputs.
	Size int64
}

// Stream is a per-stream subset of ffprobe's streams[] array.
type Stream struct {
	// Codec is codec_name — e.g. "h264", "hevc", "aac".
	Codec string

	// Duration is the stream-level duration. Not always
	// present; ffprobe reports it per-stream on demuxable
	// containers but may leave it unset after a first-pass
	// remux.
	Duration float64

	// Width/Height are pixels for video streams; 0 for audio.
	Width  int
	Height int

	// SampleRate is Hz for audio streams; 0 for video.
	SampleRate int
}

// Run returns ffprobe output + stat'd file size. Any non-zero
// ffprobe exit surfaces as an error with stderr in the message;
// a zero exit with malformed JSON is treated the same — we
// don't try to recover from a broken ffprobe.
func (p *Probe) Run(ctx context.Context, path string) (*Result, error) {
	log := p.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	runner := p.Runner
	if runner == nil {
		runner = execRunner{}
	}
	bin := p.FFprobePath
	if bin == "" {
		bin = DefaultFFprobePath
	}

	args := []string{
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}

	var stdout, stderr bytes.Buffer
	runErr := runner.Run(ctx, bin, args, &stdout, &stderr)
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return nil, runErr
		}
		return nil, fmt.Errorf("probe: ffprobe failed: %w\nstderr:\n%s", runErr, truncate(stderr.String(), 4<<10))
	}

	result, err := parseProbeOutput(stdout.Bytes())
	if err != nil {
		log.Warn("ffprobe produced unparseable output",
			"path", path,
			"stdout", truncate(stdout.String(), 1<<10))
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("probe: stat %s: %w", path, err)
	}
	result.Size = info.Size()

	return result, nil
}

// parseProbeOutput decodes ffprobe's JSON into Result. Isolated
// so tests can feed canned JSON without constructing a Runner.
func parseProbeOutput(data []byte) (*Result, error) {
	// ffprobe emits durations as strings ("12.345000") not
	// numbers. Decode into strings and atof them.
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecName  string `json:"codec_name"`
			CodecType  string `json:"codec_type"`
			Duration   string `json:"duration"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("probe: decode ffprobe json: %w", err)
	}

	out := &Result{}
	// Some containers report format.duration as "N/A" on early
	// demuxable-but-incomplete files. Silently treat unparseable
	// values as zero — same behavior as stream.duration below —
	// so the corruption check can interpret zero as "can't
	// measure, skip healing" per spec.
	if raw.Format.Duration != "" {
		if d, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
			out.Duration = d
		}
	}

	for _, s := range raw.Streams {
		stream := &Stream{
			Codec:  s.CodecName,
			Width:  s.Width,
			Height: s.Height,
		}
		if s.Duration != "" {
			// stream.Duration may be "N/A" on some containers;
			// treat anything unparseable as unset (0) rather
			// than erroring out the whole probe.
			if d, err := strconv.ParseFloat(s.Duration, 64); err == nil {
				stream.Duration = d
			}
		}
		if s.SampleRate != "" {
			if rate, err := strconv.Atoi(s.SampleRate); err == nil {
				stream.SampleRate = rate
			}
		}

		switch s.CodecType {
		case "video":
			if out.VideoStream == nil {
				out.VideoStream = stream
			}
		case "audio":
			if out.AudioStream == nil {
				out.AudioStream = stream
			}
		}
	}

	return out, nil
}

// truncate clips s to n bytes with an indicator. Kept local so
// the probe package doesn't take a cross-sibling dep on
// remux/helpers for one string function.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}

// execRunner is the production Runner: os/exec with stdout +
// stderr piped to the caller's buffers.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
