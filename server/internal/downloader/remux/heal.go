package remux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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
// format.duration doesn't match its stream duration. Like Run,
// writes to outputPath+".part" first and atomic-renames on
// success; on failure or cancellation the partial output is
// removed.
//
// Output is a caller-provided path rather than an in-place
// overwrite — the caller keeps the un-healed input until it
// confirms the heal produced a better file. Spec's "log and
// continue, a partial VOD is better than none" policy relies
// on having both files available at decision time.
//
// Audio mode passes `-c:a copy` so ffmpeg doesn't try to copy
// a non-existent video stream; video mode passes `-c copy`
// which matches Run.
func (r *Remuxer) Heal(ctx context.Context, inputPath, outputPath string, kind Kind) error {
	log := r.logOrDiscard()
	runner := r.runnerOrExec()
	bin := r.binOrDefault()

	partPath := outputPath + partSuffix
	args := healArgs(inputPath, partPath, kind)

	// Same cleanup pattern as Run: defer os.Remove gated on a
	// committed flag so panic / rename-failure / ffmpeg-failure
	// all land the .part in the bin.
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
		log.Warn("ffmpeg heal failed",
			"input", inputPath,
			"output", outputPath,
			"kind", kind,
			"stderr", preview)
		return fmt.Errorf("remux heal: ffmpeg failed: %w\nstderr:\n%s", runErr, preview)
	}

	if err := os.Rename(partPath, outputPath); err != nil {
		return fmt.Errorf("remux heal: commit rename %s → %s: %w", partPath, outputPath, err)
	}
	committed = true
	return nil
}

// healArgs returns the argv for the heal pass. Audio jobs use
// `-c:a copy` so ffmpeg doesn't complain about the missing video
// stream; video jobs use `-c copy` (all streams) to stay
// consistent with Run.
func healArgs(inputPath, outputPath string, kind Kind) []string {
	if kind == KindAudio {
		return []string{
			"-y",
			"-i", inputPath,
			"-c:a", "copy",
			outputPath,
		}
	}
	return []string{
		"-y",
		"-i", inputPath,
		"-c", "copy",
		outputPath,
	}
}
