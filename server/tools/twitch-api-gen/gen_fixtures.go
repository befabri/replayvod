package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/PuerkitoBio/goquery"
)

// generateNormalizeFixtures writes one input/expected HTML pair per entry in
// normalizeFixes, sourcing pristine input from the committed reference
// snapshot. Each fix is applied against its own freshly-parsed doc so fixture
// pairs are order-independent.
//
// Fires via `go run ./tools/twitch-api-gen -gen-fixtures`. The output is
// committed and consumed by TestNormalize_PerFix in normalize_test.go.
func generateNormalizeFixtures(log *slog.Logger) error {
	const (
		snapshotPath = "./tools/twitch-api-gen/testdata/reference-snapshot.html"
		outDir       = "./tools/twitch-api-gen/testdata/normalize"
	)

	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	for _, fix := range normalizeFixes {
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("parse snapshot for %s: %w", fix.Name, err)
		}
		el := resolveScope(doc, fix.Scope, fix.Endpoint)
		if el == nil {
			return fmt.Errorf("fix %s: element not found in snapshot", fix.Name)
		}

		inputHTML, err := goquery.OuterHtml(el)
		if err != nil {
			return fmt.Errorf("fix %s: serialize input: %w", fix.Name, err)
		}

		if err := fix.Apply(el, log); err != nil {
			return fmt.Errorf("fix %s: apply: %w", fix.Name, err)
		}
		expectedHTML, err := goquery.OuterHtml(el)
		if err != nil {
			return fmt.Errorf("fix %s: serialize expected: %w", fix.Name, err)
		}

		inputPath := filepath.Join(outDir, fix.Name+".input.html")
		expectedPath := filepath.Join(outDir, fix.Name+".expected.html")
		if err := os.WriteFile(inputPath, []byte(inputHTML), 0o644); err != nil {
			return fmt.Errorf("fix %s: write input: %w", fix.Name, err)
		}
		if err := os.WriteFile(expectedPath, []byte(expectedHTML), 0o644); err != nil {
			return fmt.Errorf("fix %s: write expected: %w", fix.Name, err)
		}
		log.Info("wrote fixture pair", "fix", fix.Name, "input_bytes", len(inputHTML), "expected_bytes", len(expectedHTML))
	}
	log.Info("generated normalize fixtures", "count", len(normalizeFixes), "dir", outDir)
	return nil
}
