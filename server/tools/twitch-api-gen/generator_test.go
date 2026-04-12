package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// snapshotTimestamp is used when rendering the snapshot-test fixtures so the
// `// Generated:` header in testdata/expected/ stays stable.
var snapshotTimestamp = time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)

// loadSnapshot parses the committed reference-snapshot.html fixture.
func loadSnapshot(t *testing.T) *goquery.Document {
	t.Helper()
	path := filepath.Join("testdata", "reference-snapshot.html")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	return doc
}

// silentLogger discards all output so warnings from normalization don't clutter test logs.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestSnapshot_generatesExpectedOutput(t *testing.T) {
	doc := loadSnapshot(t)
	log := silentLogger()

	Normalize(doc, log)
	defs, err := ParseAll(doc, endpoints, log)
	if err != nil {
		t.Fatalf("parse all: %v", err)
	}

	outDir := t.TempDir()
	if err := Generate(defs, GenerateOptions{
		OutDir:    outDir,
		SourceURL: "https://dev.twitch.tv/docs/api/reference/",
		Timestamp: snapshotTimestamp,
		Log:       log,
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, name := range []string{"generated_types.go", "generated_client.go"} {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("read generated %s: %v", name, err)
		}
		want, err := os.ReadFile(filepath.Join("testdata", "expected", name))
		if err != nil {
			t.Fatalf("read expected %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from testdata/expected; run `task twitch-api-gen:regen-snapshot` to update", name)
			// Write the got output next to the expected for easier diffing.
			_ = os.WriteFile(filepath.Join(outDir, name+".got"), got, 0o644)
			t.Logf("got output saved to %s", filepath.Join(outDir, name+".got"))
		}
	}
}

func TestGenerate_isIdempotent(t *testing.T) {
	doc := loadSnapshot(t)
	log := silentLogger()
	Normalize(doc, log)
	defs, err := ParseAll(doc, endpoints, log)
	if err != nil {
		t.Fatalf("parse all: %v", err)
	}

	runOnce := func() (string, string) {
		dir := t.TempDir()
		if err := Generate(defs, GenerateOptions{
			OutDir:    dir,
			SourceURL: "https://dev.twitch.tv/docs/api/reference/",
			Timestamp: snapshotTimestamp,
			Log:       log,
		}); err != nil {
			t.Fatalf("generate: %v", err)
		}
		readAll := func(name string) string {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return string(b)
		}
		return readAll("generated_types.go"), readAll("generated_client.go")
	}

	a1, b1 := runOnce()
	a2, b2 := runOnce()
	if a1 != a2 {
		t.Errorf("generated_types.go not idempotent")
	}
	if b1 != b2 {
		t.Errorf("generated_client.go not idempotent")
	}
}

// Sanity-check: ensure the normalization pass doesn't leak errors and that the
// master reference table has something in it.
func TestParseAll_snapshotProducesAllFilteredEndpoints(t *testing.T) {
	_ = context.Background
	doc := loadSnapshot(t)
	log := silentLogger()
	Normalize(doc, log)
	defs, err := ParseAll(doc, endpoints, log)
	if err != nil {
		t.Fatalf("parse all: %v", err)
	}
	if len(defs) != len(endpoints) {
		t.Errorf("parsed %d endpoints; want %d", len(defs), len(endpoints))
	}
	ids := map[string]EndpointDef{}
	for _, ep := range defs {
		ids[ep.ID] = ep
	}
	u, ok := ids["get-users"]
	if !ok {
		t.Fatalf("get-users missing from parsed set")
	}
	if u.Method != "GET" || u.Path != "/users" {
		t.Errorf("get-users method/path = %q %q; want GET /users", u.Method, u.Path)
	}
	// Response should be `data` (Object[]) containing User fields.
	if len(u.Response) == 0 || u.Response[0].Name != "data" {
		t.Fatalf("get-users response doesn't start with data[]: %+v", u.Response)
	}
	fieldNames := map[string]bool{}
	for _, c := range u.Response[0].Children {
		fieldNames[c.Name] = true
	}
	for _, want := range []string{"id", "login", "display_name", "email", "created_at"} {
		if !fieldNames[want] {
			t.Errorf("get-users missing field %q", want)
		}
	}
}
