package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// checkAgainstCommitted compares the freshly-generated files in generatedDir
// to the committed copies in committedDir, ignoring the `// Generated:`
// timestamp line. Returns an error that lists the stale files; callers should
// print the error and exit non-zero so CI fails when someone commits a
// generator change without regenerating.
//
// Files compared (relative names): generated_client.go, generated_eventsub.go,
// generated_types.go. A missing committed file is treated as a mismatch.
func checkAgainstCommitted(generatedDir, committedDir string, log *slog.Logger) error {
	names := []string{"generated_client.go", "generated_eventsub.go", "generated_types.go"}
	var stale []string
	for _, name := range names {
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
		return fmt.Errorf("generated files out of date: %s\nrun `task twitch-api-gen` and commit the diff", strings.Join(stale, ", "))
	}
	log.Info("check: generated files up to date", "dir", committedDir, "files", len(names))
	return nil
}

// stripTimestampLine removes the single `// Generated: …` header line so
// `-check` tolerates wall-clock regen timestamps; everything else (including
// the Source comment) stays byte-identical.
func stripTimestampLine(b []byte) []byte {
	lines := bytes.SplitN(b, []byte{'\n'}, 4)
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("// Generated:")) {
			lines[i] = []byte("// Generated: <stripped>")
			break
		}
	}
	return bytes.Join(lines, []byte{'\n'})
}
