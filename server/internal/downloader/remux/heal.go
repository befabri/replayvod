package remux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

// CorruptionThreshold is the format/stream duration mismatch, in seconds,
// beyond which a stream-copy repair is attempted.
const CorruptionThreshold = 50.0

// Heal stream-copies input into outputPath, committing through files after success.
// The caller retains inputPath until the repaired output passes validation.
// Failure removes partial output and preserves both execution and cleanup errors.
func (r *Remuxer) Heal(ctx context.Context, inputPath, outputPath string, kind Kind, files FileOperations) (err error) {
	log := r.logOrDiscard()
	runner := r.runnerOrExec()
	bin := r.binOrDefault()
	files = filesOrLocal(files)

	partPath := outputPath + partSuffix
	args := healArgs(inputPath, partPath, kind)
	if err := removePartial(files, partPath); err != nil {
		return fmt.Errorf("remux heal: remove stale partial output: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if cleanupErr := removePartial(files, partPath); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remux heal: remove partial output: %w", cleanupErr))
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
		log.Warn("ffmpeg heal failed",
			"input", inputPath,
			"output", outputPath,
			"kind", kind,
			"stderr", preview)
		return fmt.Errorf("remux heal: ffmpeg failed: %w\nstderr:\n%s", runErr, preview)
	}

	if err := files.Rename(partPath, outputPath); err != nil {
		return fmt.Errorf("remux heal: commit rename %s → %s: %w", partPath, outputPath, err)
	}
	committed = true
	return nil
}

func healArgs(inputPath, outputPath string, kind Kind) []string {
	if kind == KindAudio {
		return []string{
			"-y",
			"-i", inputPath,
			"-c:a", "copy",
			"-f", "mp4",
			outputPath,
		}
	}
	return []string{
		"-y",
		"-i", inputPath,
		"-c", "copy",
		"-f", "mp4",
		outputPath,
	}
}
