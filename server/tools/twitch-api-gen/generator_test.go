package main

import (
	"bytes"
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

func loadDoc(t *testing.T, name string) *goquery.Document {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

func loadSnapshot(t *testing.T) *goquery.Document {
	return loadDoc(t, "reference-snapshot.html")
}

// silentLogger discards all output so normalize warnings don't clutter tests.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// snapshotPipeline runs the full scraper → generator flow against committed
// fixtures and returns the output directory.
func snapshotPipeline(t *testing.T) string {
	t.Helper()
	log := silentLogger()

	ref := loadSnapshot(t)
	Normalize(ref, log)
	defs, err := ParseAll(ref, endpoints, log)
	if err != nil {
		t.Fatalf("parse all: %v", err)
	}

	evRef := loadDoc(t, "eventsub-reference-snapshot.html")
	evTypes := loadDoc(t, "eventsub-subscription-types-snapshot.html")
	esRef, esSubs, err := ParseEventSubReference(evRef, evTypes, log)
	if err != nil {
		t.Fatalf("parse eventsub: %v", err)
	}

	outDir := t.TempDir()
	if err := Generate(defs, GenerateOptions{
		OutDir:            outDir,
		SourceURL:         "https://dev.twitch.tv/docs/api/reference/",
		Timestamp:         snapshotTimestamp,
		EventSubReference: esRef,
		EventSubSubs:      esSubs,
		Log:               log,
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	return outDir
}

// generatedFilenames lists every file Generate produces when EventSub input is present.
var generatedFilenames = []string{
	"generated_types.go",
	"generated_client.go",
	"generated_eventsub.go",
}

func TestSnapshot_generatesExpectedOutput(t *testing.T) {
	outDir := snapshotPipeline(t)
	for _, name := range generatedFilenames {
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
			_ = os.WriteFile(filepath.Join(outDir, name+".got"), got, 0o644)
			t.Logf("got output saved to %s", filepath.Join(outDir, name+".got"))
		}
	}
}

// Sanity-check the parsed endpoint set for get-users in isolation.
func TestParseAll_snapshotProducesAllFilteredEndpoints(t *testing.T) {
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

// TestEventSubScraper_resolvesKnownTypes validates the two-page EventSub
// pipeline against the committed fixtures. If the scraper silently loses a
// known subscription type in the future, this test catches it.
func TestEventSubScraper_resolvesKnownTypes(t *testing.T) {
	log := silentLogger()
	evRef := loadDoc(t, "eventsub-reference-snapshot.html")
	evTypes := loadDoc(t, "eventsub-subscription-types-snapshot.html")
	ref, subs, err := ParseEventSubReference(evRef, evTypes, log)
	if err != nil {
		t.Fatalf("parse eventsub: %v", err)
	}
	if len(ref.Conditions) == 0 {
		t.Fatal("no conditions parsed")
	}
	if len(ref.Events) == 0 {
		t.Fatal("no events parsed")
	}
	if len(subs) < 50 {
		t.Errorf("only %d subscription types resolved; expected >= 50", len(subs))
	}

	// Spot-check a few well-known types resolve to the expected anchors.
	want := map[string]string{
		"stream.online":  "stream-online-condition",
		"channel.follow": "channel-follow-condition",
		"channel.update": "channel-update-condition",
	}
	for _, s := range subs {
		if expect, ok := want[s.Type]; ok {
			if s.ConditionAnchor != expect {
				t.Errorf("%s condition anchor = %q; want %q", s.Type, s.ConditionAnchor, expect)
			}
			delete(want, s.Type)
		}
	}
	for typ := range want {
		t.Errorf("%s not resolved by scraper", typ)
	}
}

// TestIsDeprecatedField locks each entry in deprecatedFieldMarkers against
// the verbatim phrasings pulled from the committed reference snapshot.
// Descriptions are HTML-stripped by tableschema.go (<strong> → plain text),
// so the positives here have no `**` markers — matches what isDeprecatedField
// actually sees at runtime.
func TestIsDeprecatedField(t *testing.T) {
	positives := []string{
		// Stream.TagIds, Video.TagIds (Feb 28 deprecation).
		`IMPORTANT As of February 28, 2023, this field is deprecated and returns only an empty array. If you use this field, please update your code to use the tags field.`,
		// Stream.IsMature, Video.IsMature (is_mature always false).
		`IMPORTANT This field is deprecated and returns only false. A Boolean value that indicates whether the stream is meant for mature audiences.`,
		// User.ViewCount.
		`The number of times the user's channel has been viewed. NOTE: This field has been deprecated (see Get Users API endpoint – "view_count" deprecation). Any data in this field is not valid and should not be used.`,
	}
	negatives := []string{
		"This field is no longer recommended.",
		"Deprecated soon — migrate to the new endpoint.",
		"Partially deprecated but still in use.",
		"",
		"The user's ID.",
	}
	for _, d := range positives {
		if !isDeprecatedField(d) {
			t.Errorf("expected match: %q", d)
		}
	}
	for _, d := range negatives {
		if isDeprecatedField(d) {
			t.Errorf("unexpected match: %q", d)
		}
	}
}
