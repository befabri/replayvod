package main

import "testing"

func TestSchemaGoName(t *testing.T) {
	cases := []struct {
		anchor string
		want   string
	}{
		// Plural anchors → singular struct name.
		{"outcomes", "Outcome"},
		{"choices", "Choice"},
		{"top_predictors", "TopPredictor"},
		{"top_contributions", "TopContribution"},

		// Singular anchors → PascalCase as-is.
		{"reward", "Reward"},
		{"image", "Image"},
		{"product", "Product"},
		{"message", "Message"},

		// Anchors in the singularAnchors override: ends in 's' but already singular.
		{"max_per_stream", "MaxPerStream"},
		{"max_per_user_per_stream", "MaxPerUserPerStream"},
		{"bits_voting", "BitsVoting"},
		{"channel_points_voting", "ChannelPointsVoting"},
		{"global_cooldown", "GlobalCooldown"},
	}
	for _, c := range cases {
		got := schemaGoName(c.anchor)
		if got != c.want {
			t.Errorf("schemaGoName(%q) = %q; want %q", c.anchor, got, c.want)
		}
	}
}

func TestIsPluralAnchor(t *testing.T) {
	plural := []string{"outcomes", "choices", "top_predictors", "top_contributions"}
	singular := []string{"reward", "image", "product", "max_per_stream", "bits_voting", "global_cooldown"}

	for _, a := range plural {
		if !isPluralAnchor(a) {
			t.Errorf("isPluralAnchor(%q) = false; want true", a)
		}
	}
	for _, a := range singular {
		if isPluralAnchor(a) {
			t.Errorf("isPluralAnchor(%q) = true; want false", a)
		}
	}
}

// TestNamedSchemaResolver_resolve exercises the resolver against a minimal
// in-memory EventSubReference. Covers: plural → []T, singular → T, miss → empty.
func TestNamedSchemaResolver_resolve(t *testing.T) {
	ref := &EventSubReference{
		NamedSchemas: map[string]EventSubSchema{
			"outcomes":       {AnchorID: "outcomes"},
			"image":          {AnchorID: "image"},
			"max-per-stream": {AnchorID: "max-per-stream"}, // anchor is hyphenated on the page
		},
	}
	// The singularAnchors set uses underscored keys; test that match too.
	// (The resolver normalizes `max_per_stream` → `max-per-stream` before lookup.)

	r := &namedSchemaResolver{ref: ref, reached: map[string]bool{}}

	cases := []struct {
		typeStr  string
		want     string
		wantOK   bool
	}{
		{"outcomes", "[]Outcome", true},
		{"Outcomes", "[]Outcome", true}, // case-insensitive
		{"image", "Image", true},
		{"max_per_stream", "MaxPerStream", true}, // underscore → hyphen + singular override
		{"unknown_schema", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := r.resolve(c.typeStr)
		if got != c.want || ok != c.wantOK {
			t.Errorf("resolve(%q) = (%q, %v); want (%q, %v)", c.typeStr, got, ok, c.want, c.wantOK)
		}
	}

	// Reached set should include the anchors we successfully resolved.
	for _, a := range []string{"outcomes", "image", "max-per-stream"} {
		if !r.reached[a] {
			t.Errorf("resolve was supposed to mark %q reached", a)
		}
	}
}

// TestNamedSchemaResolver_nil safety — nil receiver, nil ref.
func TestNamedSchemaResolver_nil(t *testing.T) {
	var r *namedSchemaResolver
	if got, ok := r.resolve("outcomes"); got != "" || ok {
		t.Errorf("nil receiver: resolve = (%q, %v); want (\"\", false)", got, ok)
	}
	r = &namedSchemaResolver{}
	if got, ok := r.resolve("outcomes"); got != "" || ok {
		t.Errorf("nil ref: resolve = (%q, %v); want (\"\", false)", got, ok)
	}
}

// TestReachability_Transitive asserts emitReachedNamedSchemas follows
// multi-hop refs: a seed anchor whose schema references another, which in
// turn references a third, must emit all three.
func TestReachability_Transitive(t *testing.T) {
	ref := &EventSubReference{
		NamedSchemas: map[string]EventSubSchema{
			"alpha": {AnchorID: "alpha", Fields: []FieldSchema{{Name: "b_ref", Type: "beta"}}},
			"beta":  {AnchorID: "beta", Fields: []FieldSchema{{Name: "c_ref", Type: "gamma"}}},
			"gamma": {AnchorID: "gamma", Fields: []FieldSchema{{Name: "v", Type: "String"}}},
		},
	}
	resolver := &namedSchemaResolver{ref: ref, reached: map[string]bool{"alpha": true}}
	out := emitReachedNamedSchemas(ref, resolver, &templateModel{}, silentLogger())

	got := map[string]bool{}
	for _, tm := range out {
		got[tm.AnchorID] = true
	}
	for _, a := range []string{"alpha", "beta", "gamma"} {
		if !got[a] {
			t.Errorf("anchor %q not emitted; got %v", a, got)
		}
	}
}

// TestReachability_Cycle asserts a ref cycle A ↔ B terminates and emits each
// schema once.
func TestReachability_Cycle(t *testing.T) {
	ref := &EventSubReference{
		NamedSchemas: map[string]EventSubSchema{
			"alpha": {AnchorID: "alpha", Fields: []FieldSchema{{Name: "b_ref", Type: "beta"}}},
			"beta":  {AnchorID: "beta", Fields: []FieldSchema{{Name: "a_ref", Type: "alpha"}}},
		},
	}
	resolver := &namedSchemaResolver{ref: ref, reached: map[string]bool{"alpha": true}}
	out := emitReachedNamedSchemas(ref, resolver, &templateModel{}, silentLogger())

	counts := map[string]int{}
	for _, tm := range out {
		counts[tm.AnchorID]++
	}
	for _, a := range []string{"alpha", "beta"} {
		if counts[a] != 1 {
			t.Errorf("anchor %q emitted %d times; want 1", a, counts[a])
		}
	}
}

// TestValidateManualEventAnchorOverrides_catchesMissingTarget simulates a
// typo in manualEventAnchorOverrides by running validation against a ref
// that lacks one of the real override targets. Fails loud is the contract.
func TestValidateManualEventAnchorOverrides_catchesMissingTarget(t *testing.T) {
	// Include every override target except "shield-mode" so validation must
	// complain about at least one entry.
	ref := &EventSubReference{
		NamedSchemas: map[string]EventSubSchema{
			"shoutout-create":   {AnchorID: "shoutout-create"},
			"shoutout-received": {AnchorID: "shoutout-received"},
		},
		Events: map[string]EventSubSchema{},
	}
	if err := validateManualEventAnchorOverrides(ref); err == nil {
		t.Fatalf("expected error for missing shield-mode; got nil")
	}
}

// TestValidateManualEventAnchorOverrides_acceptsPromotedTargets — a target
// already promoted into Events (by a prior override) is still considered
// valid. Mirrors what happens when shield_mode.begin moves the schema and
// shield_mode.end consults the same anchor.
func TestValidateManualEventAnchorOverrides_acceptsPromotedTargets(t *testing.T) {
	ref := &EventSubReference{
		NamedSchemas: map[string]EventSubSchema{
			"shoutout-create":   {AnchorID: "shoutout-create"},
			"shoutout-received": {AnchorID: "shoutout-received"},
		},
		Events: map[string]EventSubSchema{
			"shield-mode": {AnchorID: "shield-mode"}, // previously promoted
		},
	}
	if err := validateManualEventAnchorOverrides(ref); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestReachability_Unused asserts NamedSchemas that nothing references are
// NOT emitted — the registry doesn't leak unused types into the output.
func TestReachability_Unused(t *testing.T) {
	ref := &EventSubReference{
		NamedSchemas: map[string]EventSubSchema{
			"alpha": {AnchorID: "alpha", Fields: []FieldSchema{{Name: "v", Type: "String"}}},
			"zeta":  {AnchorID: "zeta", Fields: []FieldSchema{{Name: "x", Type: "String"}}},
		},
	}
	resolver := &namedSchemaResolver{ref: ref, reached: map[string]bool{"alpha": true}}
	out := emitReachedNamedSchemas(ref, resolver, &templateModel{}, silentLogger())

	for _, tm := range out {
		if tm.AnchorID == "zeta" {
			t.Errorf("unreferenced anchor %q unexpectedly emitted", tm.AnchorID)
		}
	}
	if len(out) != 1 {
		t.Errorf("emitted %d schemas; want only 1 (alpha)", len(out))
	}
}
