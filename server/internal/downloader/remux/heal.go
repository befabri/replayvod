package remux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// CorruptionThreshold is the duration-mismatch ceiling (seconds)
// above which Stage 9 calls the healing pass. 50s is v1's tuning —
// enough slack that normal encoder jitter doesn't trip it, tight
// enough that a genuinely truncated container gets caught.
//
// Spec Stage 9: if format.duration and the relevant stream's
// duration differ by more than this, re-mux with stream copy to
// rewrite the container's duration field. If the mismatch
// survives a heal pass, log and keep the un-healed file — a
// partial VOD is better than none.
const CorruptionThreshold = 50.0

// Heal re-runs ffmpeg with stream copy to fix a container whose
// format.duration doesn't match its video/audio stream duration.
// Common cause: the fetcher died mid-remux and the parent
// retried, leaving ffmpeg's container-level duration field stale
// relative to the actual stream bytes.
//
// Video mode runs `ffmpeg -i input -vcodec copy -acodec copy
// output`; audio mode drops the video stream flag. Both are pure
// stream copies — no decoding, no re-encoding, no quality loss.
// Typical run time is seconds even for long VODs.
//
// Output is written to a caller-provided path; Heal does not
// replace the input in place. The caller decides whether to
// rename over the original on success or keep both for
// diagnostic purposes.
//
// On ffmpeg failure, Heal returns an error with an 8 KiB stderr
// preview just like Run. ctx cancellation surfaces as the raw
// ctx error without the stderr dressing.
func (r *Remuxer) Heal(ctx context.Context, inputPath, outputPath string, kind Kind) error {
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

	args := healArgs(inputPath, outputPath, kind)

	var stderr bytes.Buffer
	runErr := runner.Run(ctx, bin, args, &stderr)
	if runErr == nil {
		return nil
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return runErr
	}

	preview := truncate(stderr.String(), 8<<10)
	log.Warn("ffmpeg heal failed",
		"input", inputPath,
		"output", outputPath,
		"kind", kind,
		"stderr", preview)
	return fmt.Errorf("remux heal: ffmpeg failed: %w\nstderr:\n%s", runErr, preview)
}

// healArgs returns the argv for the heal pass. Audio jobs drop
// the video-copy flag so ffmpeg doesn't complain about a missing
// video stream.
//
// Kept separate from Run's ffmpegArgs because the flag shape is
// different (no -f concat, single input file, different codec
// flags). Collapsing the two would mean more conditionals than
// a second function.
func healArgs(inputPath, outputPath string, kind Kind) []string {
	if kind == KindAudio {
		return []string{
			"-y",
			"-i", inputPath,
			"-acodec", "copy",
			outputPath,
		}
	}
	return []string{
		"-y",
		"-i", inputPath,
		"-vcodec", "copy",
		"-acodec", "copy",
		outputPath,
	}
}
