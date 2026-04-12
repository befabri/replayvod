package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// TestNormalize_PerFix loads the committed input/expected fixture pair for
// each fix, applies ONLY that fix to a fresh parse of the input, and asserts
// the serialized output matches the expected fixture.
//
// Pristine-per-fix inputs catch two failure modes the full-snapshot smoke
// test can't: over-matching replacements and silent behavior drift where the
// fix runs but produces different output than committed.
func TestNormalize_PerFix(t *testing.T) {
	for _, fix := range normalizeFixes {
		t.Run(fix.Name, func(t *testing.T) {
			input := readFixture(t, fix.Name+".input.html")
			expected := readFixture(t, fix.Name+".expected.html")

			doc := parseFragment(t, input)
			// Fixtures are rooted at the wrapper itself — resolveScope navigates
			// via the endpoint anchor, which lives outside a .right-code fragment.
			el, err := resolveFixtureWrapper(doc, fix.Scope)
			if err != nil {
				t.Fatalf("resolve fixture: %v", err)
			}

			if err := fix.Apply(el, silentLogger()); err != nil {
				t.Fatalf("apply: %v", err)
			}

			got, err := goquery.OuterHtml(el)
			if err != nil {
				t.Fatalf("serialize: %v", err)
			}
			if got != expected {
				t.Errorf("output mismatch\n--- expected (%d bytes)\n%s\n--- got (%d bytes)\n%s",
					len(expected), expected, len(got), got)
			}
		})
	}
}

// TestNormalize_AllFixesApply runs every fix against the full snapshot (in
// registry order, mirroring production) and asserts each reports success.
// Guards against matcher-encoding regressions that compile and emit no
// obvious diff — see hazard 14 in the plan.
func TestNormalize_AllFixesApply(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "reference-snapshot.html"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	for _, r := range RunFixes(doc, silentLogger()) {
		if r.Err != nil {
			t.Errorf("%s (%s): %v", r.Name, r.Endpoint, r.Err)
		}
	}
}

// TestNormalize_FixNamesUnique guards against two registry entries colliding
// on a filename, which would cause fixtures to overwrite each other silently.
func TestNormalize_FixNamesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, fix := range normalizeFixes {
		if seen[fix.Name] {
			t.Errorf("duplicate fix name: %s", fix.Name)
		}
		seen[fix.Name] = true
	}
}

// TestNormalize_FixturePairsMatchRegistry ensures every fixture file on disk
// corresponds to a registered fix and vice versa. Renaming a fix without
// regenerating fixtures should fail loud.
func TestNormalize_FixturePairsMatchRegistry(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "normalize"))
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if stem, ok := strings.CutSuffix(e.Name(), ".input.html"); ok {
			onDisk[stem] = true
		}
	}
	inRegistry := map[string]bool{}
	for _, fix := range normalizeFixes {
		inRegistry[fix.Name] = true
	}

	var missing, extra []string
	for name := range inRegistry {
		if !onDisk[name] {
			missing = append(missing, name)
		}
	}
	for name := range onDisk {
		if !inRegistry[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("fixtures missing on disk (run `go run ./tools/twitch-api-gen -gen-fixtures`): %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("fixtures with no matching registry entry: %v", extra)
	}
}

// TestDecodeEntitiesForMatch pins the contract directly so a future refactor
// that breaks the tag/text split surfaces here, not via a dozen per-fix test
// failures. Covers: pure text, tag-wrapped text, attribute values unchanged,
// mixed, nbsp entity, and the empty string.
func TestDecodeEntitiesForMatch(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"pure text quotes", `a "b" c`, `a &#34;b&#34; c`},
		{"pure text apostrophe", `it's here`, `it&#39;s here`},
		{"nbsp entity alone", `&nbsp;x`, "\u00a0x"},
		{"attribute quotes preserved", `<span class="x">text</span>`, `<span class="x">text</span>`},
		{"text in tagged content encoded", `<span class="x">"y"</span>`, `<span class="x">&#34;y&#34;</span>`},
		{"both attribute and text", `<a href="/x">"click"</a>`, `<a href="/x">&#34;click&#34;</a>`},
		{"nbsp inside tag value unchanged", `<td>&nbsp;x</td>`, "<td>\u00a0x</td>"},
		{"empty string", ``, ``},
		{"no-op on plain identifier", `abc`, `abc`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decodeEntitiesForMatch(c.in)
			if got != c.want {
				t.Errorf("decodeEntitiesForMatch(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

// --- helpers ---

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "normalize", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func parseFragment(t *testing.T, fragment string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		t.Fatalf("parse fragment: %v", err)
	}
	return doc
}

// resolveFixtureWrapper returns the single wrapper a fixture should contain,
// or a descriptive error. Fixtures generated by `-gen-fixtures` always hold
// exactly one `.left-docs` or `.right-code`; a hand-crafted fixture that
// deviates should fail the test with a clear message instead of silently
// picking the first match.
func resolveFixtureWrapper(doc *goquery.Document, scope fixScope) (*goquery.Selection, error) {
	var selector string
	switch scope {
	case scopeDocs:
		selector = ".left-docs"
	case scopeCode:
		selector = ".right-code"
	default:
		return nil, fmt.Errorf("unknown fix scope %d", scope)
	}
	sel := doc.Find(selector)
	switch sel.Length() {
	case 0:
		return nil, fmt.Errorf("no %s element in fixture", selector)
	case 1:
		return sel, nil
	default:
		return nil, fmt.Errorf("fixture has %d %s elements; want exactly 1", sel.Length(), selector)
	}
}
