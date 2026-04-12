package main

import "testing"

// Cases in this file use real phrasings pulled from
// testdata/reference-snapshot.html. If a regex starts misfiring in the field,
// that miss gets captured here as a table entry — invented phrasings would let
// the regex pass tests and still fail in production.

// --- Per-regex unit tests ---

func TestReStringMax(t *testing.T) {
	pos := []string{
		"The title may contain a maximum of 45 characters.",
		"The prompt is limited to a maximum of 200 characters.",
		"maximum length is 140 characters",
	}
	neg := []string{
		"The maximum number of items to return per page in the response.",
		"The default is 20.",
		"The maximum delay is 900 seconds (15 minutes).", // numeric seconds, not chars
	}
	for _, s := range pos {
		if reStringMax.FindStringSubmatch(s) == nil {
			t.Errorf("reStringMax should match: %q", s)
		}
	}
	for _, s := range neg {
		if reStringMax.FindStringSubmatch(s) != nil {
			t.Errorf("reStringMax should NOT match: %q", s)
		}
	}
}

func TestReStringRange(t *testing.T) {
	pos := []string{
		"Between 1 and 140 characters long",
		"from 3 to 50 characters",
	}
	neg := []string{
		"between 1 and 100 IDs",     // wrong noun
		"Range: 0 - 900",            // numeric keyword, not chars
	}
	for _, s := range pos {
		if reStringRange.FindStringSubmatch(s) == nil {
			t.Errorf("reStringRange should match: %q", s)
		}
	}
	for _, s := range neg {
		if reStringRange.FindStringSubmatch(s) != nil {
			t.Errorf("reStringRange should NOT match: %q", s)
		}
	}
}

func TestReNumericRange(t *testing.T) {
	pos := []string{
		"Range: 0 - 900",
		"Range: 3 - 120",
		"range:  1 – 99",                // en-dash
	}
	neg := []string{
		"The maximum delay is 900 seconds.",
		"the Bits range for this tier level is 1 through 99",
	}
	for _, s := range pos {
		if reNumericRange.FindStringSubmatch(s) == nil {
			t.Errorf("reNumericRange should match: %q", s)
		}
	}
	for _, s := range neg {
		if reNumericRange.FindStringSubmatch(s) != nil {
			t.Errorf("reNumericRange should NOT match: %q", s)
		}
	}
}

func TestReArrayMax(t *testing.T) {
	pos := []string{
		"You may specify a maximum of 100 IDs.",
		"A channel may specify a maximum of 10 tags.",
		"maximum of 50 items",
	}
	// Known phrasing we do NOT pick up (the trailing-number variant). Documented
	// here as an expected miss; if we decide to add a second regex for it later,
	// these strings move from neg → pos.
	missed := []string{
		"The maximum number of IDs you may specify is 100.",
		"The maximum number of login names you may specify is 100.",
	}
	neg := []string{
		"maximum of 45 characters", // strings — handled by reStringMax
		"The default is 20.",
	}
	for _, s := range pos {
		if reArrayMax.FindStringSubmatch(s) == nil {
			t.Errorf("reArrayMax should match: %q", s)
		}
	}
	for _, s := range missed {
		if reArrayMax.FindStringSubmatch(s) != nil {
			t.Errorf("reArrayMax unexpectedly matched known-miss phrasing: %q", s)
		}
	}
	for _, s := range neg {
		if reArrayMax.FindStringSubmatch(s) != nil {
			t.Errorf("reArrayMax should NOT match: %q", s)
		}
	}
}

func TestRePositiveInt(t *testing.T) {
	pos := []string{
		"The cost of the reward. Must be a positive integer.",
		"must be a positive integer and greater than 0",
	}
	neg := []string{
		"positive number",
		"a positive value",
	}
	for _, s := range pos {
		if !rePositiveInt.MatchString(s) {
			t.Errorf("rePositiveInt should match: %q", s)
		}
	}
	for _, s := range neg {
		if rePositiveInt.MatchString(s) {
			t.Errorf("rePositiveInt should NOT match: %q", s)
		}
	}
}

func TestTwoLetter_langFieldsNotConstrained(t *testing.T) {
	f := FieldSchema{
		Name: "broadcaster_language", Type: "String",
		Description: "Must be a two-letter language code. Set to other if not supported.",
	}
	base, dive := ExtractConstraints(f)
	if base != "" || dive != "" {
		t.Errorf("expected no constraint on two-letter language field; got base=%q dive=%q", base, dive)
	}
}

// --- ExtractConstraints integration ---

func TestExtractConstraints(t *testing.T) {
	bTrue := true

	cases := []struct {
		name     string
		field    FieldSchema
		wantBase string
		wantDive string
	}{
		{
			name: "string max",
			field: FieldSchema{
				Name: "title", Type: "String",
				Description: "The title may contain a maximum of 45 characters.",
			},
			wantBase: "max=45",
		},
		{
			name: "string range inline",
			field: FieldSchema{
				Name: "title", Type: "String",
				Description: "Between 1 and 140 characters long.",
			},
			wantBase: "min=1,max=140",
		},
		{
			name: "numeric range keyword",
			field: FieldSchema{
				Name: "slow_mode_wait_time", Type: "Integer",
				Description: "The wait time. Range: 3 - 120 seconds.",
			},
			wantBase: "min=3,max=120",
		},
		{
			name: "positive integer on numeric type",
			field: FieldSchema{
				Name: "cost", Type: "Integer", Required: &bTrue,
				Description: "The cost. Must be a positive integer.",
			},
			wantBase: "min=1",
		},
		{
			name: "positive integer phrase on string type does not fire",
			field: FieldSchema{
				Name: "note", Type: "String",
				Description: "must be a positive integer string",
			},
			// isNumericType("String") is false → min=1 skipped.
			wantBase: "",
		},
		{
			// Intentional: no constraint emitted. See TestTwoLetter_langFieldsNotConstrained.
			name: "two-letter code on scalar string — deliberately unconstrained",
			field: FieldSchema{
				Name: "broadcaster_language", Type: "String",
				Description: "Must be a two-letter language code.",
			},
			wantBase: "",
		},
		{
			name: "array id max",
			field: FieldSchema{
				Name: "id", Type: "String",
				Description: "You may specify a maximum of 100 IDs.",
			},
			// IsArrayParam fires on "you may specify a maximum of", so this is an
			// array even though Type is scalar String.
			wantBase: "max=100",
		},
		{
			name: "array + element bound combined",
			field: FieldSchema{
				Name: "tags", Type: "String[]",
				Description: "A channel may specify a maximum of 10 tags. Each tag is limited to a maximum of 25 characters.",
			},
			wantBase: "max=10",
			wantDive: "max=25",
		},
		{
			name: "enum appended for scalar",
			field: FieldSchema{
				Name: "period", Type: "String",
				EnumValues:  []any{"all", "day", "week", "month"},
				Description: "Possible values are: all, day, week, month.",
			},
			wantBase: "oneof=all day week month",
		},
		{
			name: "enum skipped when value contains spaces",
			field: FieldSchema{
				Name: "x", Type: "String",
				EnumValues: []any{"has space", "fine"},
			},
			// formatEnumOneof bails on whitespace — we'd rather skip than emit a
			// broken tag.
			wantBase: "",
		},
		{
			name: "negative — default is N (not a constraint)",
			field: FieldSchema{
				Name: "first", Type: "Integer",
				Description: "The maximum number of items to return per page. The default is 20.",
			},
			wantBase: "",
		},
		{
			name: "negative — numeric max in prose without Range keyword",
			field: FieldSchema{
				Name: "delay", Type: "Integer",
				Description: "The maximum delay is 900 seconds (15 minutes).",
			},
			// We miss this intentionally (no "Range:" keyword; no "characters").
			// Twitch's 400 covers it.
			wantBase: "",
		},
		{
			name: "negative — no constraint at all",
			field: FieldSchema{
				Name: "user_id", Type: "String",
				Description: "The ID of the user.",
			},
			wantBase: "",
		},
		{
			name: "negative — informational prose about ranges",
			field: FieldSchema{
				Name: "min_bits", Type: "Integer",
				Description: "For example, if min_bits is 1 and the next tier is 100, the Bits range for this tier is 1 through 99.",
			},
			wantBase: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, dive := ExtractConstraints(tc.field)
			if base != tc.wantBase {
				t.Errorf("base = %q; want %q", base, tc.wantBase)
			}
			if dive != tc.wantDive {
				t.Errorf("dive = %q; want %q", dive, tc.wantDive)
			}
		})
	}
}

func TestComposeValidateTag(t *testing.T) {
	cases := []struct {
		name     string
		forReq   bool
		required bool
		goType   string
		base     string
		dive     string
		want     string
	}{
		{"response side — no tag", false, true, "string", "max=10", "", ""},
		{"request, no constraint — no tag", true, false, "string", "", "", ""},
		{"optional with base", true, false, "string", "max=140", "", "omitempty,max=140"},
		{"required with base", true, true, "string", "max=140", "", "required,max=140"},
		{"required without other constraint", true, true, "string", "", "", "required"},
		{"optional with base + dive", true, false, "[]string", "max=10", "max=25", "omitempty,max=10,dive,max=25"},
		{"optional with only dive", true, false, "[]string", "", "max=25", "omitempty,dive,max=25"},
		// Bug A: validator's `required` rejects the zero value, and `false` is the
		// zero value for bool — which is a legitimate payload (is_enabled=false
		// disables a setting). Generator drops `required` for bool fields.
		{"required bool — skipped, no tag", true, true, "bool", "", "", ""},
		{"required bool + other constraint — drops required, keeps omitempty", true, true, "bool", "max=1", "", "omitempty,max=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := composeValidateTag(tc.forReq, tc.required, tc.goType, tc.base, tc.dive)
			if got != tc.want {
				t.Errorf("composeValidateTag = %q; want %q", got, tc.want)
			}
		})
	}
}
