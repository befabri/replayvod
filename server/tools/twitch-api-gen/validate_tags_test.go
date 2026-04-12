package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSnapshot_validateTagsOnKnownFields asserts that specific request-side
// fields in the committed `expected/generated_types.go` carry the exact
// validate tag we expect. Complements the byte-diff snapshot test with
// named-field precision — when this fails, the error points at the
// offending struct+field instead of producing a 3KB unified diff.
//
// Also enforces the negative invariant: response types (User, Game, Stream…)
// must NOT carry any validate tag. Constraint extraction is request-side only.
func TestSnapshot_validateTagsOnKnownFields(t *testing.T) {
	tags := parseGeneratedTags(t, filepath.Join("testdata", "expected", "generated_types.go"))

	// Positive — each of these fields must have the exact validate tag listed.
	// Empty string means "no validate tag present". Phrasings change on the
	// Twitch side; when the snapshot is regenerated, this table may need to
	// follow.
	positives := map[[2]string]string{
		{"ModifyChannelInformationBody", "Tags"}: "omitempty,max=10,dive,max=25",

		// Single required param — no mutual-exclusion override.
		{"GetChannelInformationParams", "BroadcasterID"}: "required,max=100",

		// Mutually-exclusive required params (get-games/get-videos accept
		// exactly one of id/name/igdb_id) — generator downgrades to optional
		// via mutuallyExclusiveParamEndpoints.
		{"GetGamesParams", "ID"}:       "omitempty,max=100",
		{"GetGamesParams", "IGDBID"}:   "omitempty,max=100",
		{"GetVideosParams", "ID"}:      "omitempty,max=100",
		{"GetStreamsParams", "UserID"}: "omitempty,max=100",

		// Required field with no extracted constraint → bare `required`.
		{"CreateEventSubSubscriptionBody", "Type"}:    "required",
		{"CreateEventSubSubscriptionBody", "Version"}: "required",
		{"DeleteEventSubSubscriptionParams", "ID"}:    "required",
	}
	for k, want := range positives {
		got := tags[k[0]][k[1]]
		if got != want {
			t.Errorf("%s.%s validate = %q; want %q", k[0], k[1], got, want)
		}
	}

	// Known under-extraction: these fields have constraint-like wording in the
	// docs that our v1 regexes intentionally don't match. If we later tighten
	// the regex set, move the entry to the positives table.
	knownMisses := [][2]string{
		{"GetUsersParams", "ID"},                  // "maximum number of IDs ... is 100"
		{"GetUsersParams", "Login"},               // same wording
		{"ModifyChannelInformationBody", "Delay"}, // "maximum delay is 900 seconds"
		{"ModifyChannelInformationBody", "Title"}, // no documented max
		// BroadcasterLanguage intentionally unconstrained — "other" is a
		// documented sentinel that `len=2` would reject. See constraints_test.go.
		{"ModifyChannelInformationBody", "BroadcasterLanguage"},
	}
	for _, k := range knownMisses {
		if got := tags[k[0]][k[1]]; got != "" {
			t.Errorf("%s.%s unexpectedly has validate tag %q — update knownMisses table", k[0], k[1], got)
		}
	}

	// Negative — response types must never carry a validate tag.
	responseTypes := []string{"User", "Game", "Stream", "Video", "ChannelInformation", "FollowedChannel"}
	for _, typ := range responseTypes {
		fields, ok := tags[typ]
		if !ok {
			continue // type not in filter
		}
		for name, tag := range fields {
			if tag != "" {
				t.Errorf("response type %s.%s must not have validate tag; got %q", typ, name, tag)
			}
		}
	}
}

// parseGeneratedTags parses the generated source with go/ast and returns a
// [structName][fieldName] → validate-tag-body map. An entry exists for every
// exported field; its value is "" when the field has no `validate:""` tag.
func parseGeneratedTags(t *testing.T, path string) map[string]map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	out := map[string]map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fields := map[string]string{}
			for _, field := range st.Fields.List {
				if field.Tag == nil {
					for _, n := range field.Names {
						fields[n.Name] = ""
					}
					continue
				}
				tag := strings.Trim(field.Tag.Value, "`")
				v := reflect.StructTag(tag).Get("validate")
				for _, n := range field.Names {
					fields[n.Name] = v
				}
			}
			out[ts.Name.Name] = fields
		}
	}
	return out
}
