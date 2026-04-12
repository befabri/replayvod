package main

import (
	"path/filepath"
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
	tags := loadFieldInfo(t, filepath.Join("testdata", "expected", "generated_types.go"))

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

		// Trailing-number array-max phrasing picked up by reArrayMaxTrailing.
		{"GetUsersParams", "ID"}:    "omitempty,max=100",
		{"GetUsersParams", "Login"}: "omitempty,max=100",

		// Numeric-max-in-prose picked up by reNumericMaxPhrase.
		{"ModifyChannelInformationBody", "Delay"}: "omitempty,max=900",

		// Required field with no extracted constraint → bare `required`.
		{"CreateEventSubSubscriptionBody", "Type"}:    "required",
		{"CreateEventSubSubscriptionBody", "Version"}: "required",
		{"DeleteEventSubSubscriptionParams", "ID"}:    "required",
	}
	for k, want := range positives {
		got := tags[k[0]][k[1]].ValidateTag
		if got != want {
			t.Errorf("%s.%s validate = %q; want %q", k[0], k[1], got, want)
		}
	}

	// Known under-extraction: these fields have constraint-like wording in the
	// docs that our v1 regexes intentionally don't match. If we later tighten
	// the regex set, move the entry to the positives table.
	knownMisses := [][2]string{
		{"ModifyChannelInformationBody", "Title"}, // no documented max
		// BroadcasterLanguage intentionally unconstrained — "other" is a
		// documented sentinel that `len=2` would reject. See constraints_test.go.
		{"ModifyChannelInformationBody", "BroadcasterLanguage"},
	}
	for _, k := range knownMisses {
		if got := tags[k[0]][k[1]].ValidateTag; got != "" {
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
		for name, fi := range fields {
			if fi.ValidateTag != "" {
				t.Errorf("response type %s.%s must not have validate tag; got %q", typ, name, fi.ValidateTag)
			}
		}
	}
}

// TestSnapshot_namedSchemaFieldTypes verifies that cross-schema references
// emit as typed Go fields, not `any`. Named-schema refs on the Twitch
// eventsub-reference page (outcomes, choices, image, reward, etc.) are
// resolved via namedSchemaResolver during generation; this test guards the
// emitted Go type on a handful of representative fields.
func TestSnapshot_namedSchemaFieldTypes(t *testing.T) {
	fieldTypes := loadFieldInfo(t, filepath.Join("testdata", "expected", "generated_eventsub.go"))

	want := map[[2]string]string{
		// Plural anchor → array of singular struct.
		{"ChannelPredictionBeginEvent", "Outcomes"}: "[]Outcome",
		{"ChannelPollBeginEvent", "Choices"}:        "[]Choice",

		// Singular anchor → scalar struct (BitsVoting, ChannelPointsVoting,
		// Image are all in singularAnchors overrides or genuinely singular).
		{"ChannelPollBeginEvent", "BitsVoting"}:          "BitsVoting",
		{"ChannelPollBeginEvent", "ChannelPointsVoting"}: "ChannelPointsVoting",
	}
	for k, w := range want {
		got, ok := fieldTypes[k[0]][k[1]]
		if !ok {
			t.Errorf("%s.%s not found in generated source", k[0], k[1])
			continue
		}
		if got.GoType != w {
			t.Errorf("%s.%s Go type = %q; want %q", k[0], k[1], got.GoType, w)
		}
	}

	// Negative assertion — no field on these structs should be `any` or `[]any`.
	// Cross-schema resolution should have replaced every previously-`any` ref.
	for _, structName := range []string{
		"ChannelPredictionBeginEvent",
		"ChannelPollBeginEvent",
		"ChannelPointsCustomRewardAddEvent",
	} {
		for fieldName, fi := range fieldTypes[structName] {
			if fi.GoType == "any" || fi.GoType == "[]any" {
				t.Errorf("%s.%s is %q — cross-schema resolver should have typed this", structName, fieldName, fi.GoType)
			}
		}
	}
}

// loadFieldInfo wraps parseGeneratedTypes for the snapshot tests, failing the
// test on parse errors so call-sites stay free of boilerplate.
func loadFieldInfo(t *testing.T, path string) map[string]map[string]fieldInfo {
	t.Helper()
	out, err := parseGeneratedTypes(path)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}
