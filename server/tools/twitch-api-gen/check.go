package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ErrGeneratedFilesStale wraps the drift-detected error so callers can
// distinguish "someone forgot to regen" (CI-signal, exit 2) from an infra
// failure in the check pipeline itself (exit 1).
var ErrGeneratedFilesStale = errors.New("generated files out of date")

// checkedFiles is the allowlist of generator outputs -check compares. Adding
// a new generated file means adding it here on purpose — a glob would pick up
// stray testdata or backup files too eagerly.
var checkedFiles = []string{"generated_client.go", "generated_eventsub.go", "generated_types.go"}

// checkAgainstCommitted compares the freshly-generated files in generatedDir
// to the committed copies in committedDir, ignoring the `// Generated:`
// timestamp line. Returns ErrGeneratedFilesStale wrapped with the list of
// stale filenames when output drifts from the committed state; returns a
// plain error for I/O failures.
//
// A missing committed file is treated as drift, not an I/O error — a new
// generator output without a committed counterpart is real missing work.
func checkAgainstCommitted(generatedDir, committedDir string, log *slog.Logger) error {
	var stale []string
	for _, name := range checkedFiles {
		gen, err := os.ReadFile(filepath.Join(generatedDir, name))
		if err != nil {
			return fmt.Errorf("read generated %s: %w", name, err)
		}
		committedPath := filepath.Join(committedDir, name)
		committed, err := os.ReadFile(committedPath)
		if err != nil {
			stale = append(stale, name+" (committed file missing)")
			continue
		}
		if !bytes.Equal(stripTimestampLine(gen), stripTimestampLine(committed)) {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("%w: %s\nrun `task twitch-api-gen` and commit the diff", ErrGeneratedFilesStale, strings.Join(stale, ", "))
	}
	log.Info("check: generated files up to date", "dir", committedDir, "files", len(checkedFiles))
	return nil
}

// stripTimestampLine rewrites the `// Generated: …` header line to a stable
// sentinel so `-check` tolerates wall-clock regen timestamps. Scans the whole
// file for the prefix — the earlier SplitN-into-4 bound was a perf
// micro-optimisation that would silently false-positive if the header ever
// grew past four lines.
func stripTimestampLine(b []byte) []byte {
	const prefix = "// Generated:"
	lines := bytes.Split(b, []byte{'\n'})
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte(prefix)) {
			lines[i] = []byte("// Generated: <stripped>")
			break
		}
	}
	return bytes.Join(lines, []byte{'\n'})
}
