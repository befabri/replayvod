package config

import "testing"

// TestContainsUnescapedQuote pins the scanner that decides whether a quoted
// dotenv value closes. It must find a quote at index 0 (the off-by-one that
// previously skipped it let an empty value `FOO=""` masquerade as a multi-line
// open), honor backslash escapes only in double-quote mode, and treat single-
// quoted values as literal.
func TestContainsUnescapedQuote(t *testing.T) {
	cases := []struct {
		name string
		s    string
		q    byte
		want bool
	}{
		{name: "quote at index 0", s: `"`, q: '"', want: true},
		{name: "quote at index 1", s: `x"`, q: '"', want: true},
		{name: "no quote", s: `abc`, q: '"', want: false},
		{name: "escaped quote is not a close", s: `\"`, q: '"', want: false},
		{name: "escaped quote then real close", s: `\"x"`, q: '"', want: true},
		{name: "single quote found", s: `a'`, q: '\'', want: true},
		{name: "backslash does not escape in single-quote mode", s: `\'`, q: '\'', want: true},
		{name: "empty", s: ``, q: '"', want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsUnescapedQuote(tc.s, tc.q); got != tc.want {
				t.Fatalf("containsUnescapedQuote(%q, %q) = %v, want %v", tc.s, tc.q, got, tc.want)
			}
		})
	}
}

// TestMultilineValueQuote pins which values godotenv will fold across physical
// lines: only a value that opens with a quote that does not close on the same
// line. An empty quoted value closes immediately and must NOT be reported as
// multi-line.
func TestMultilineValueQuote(t *testing.T) {
	cases := []struct {
		name      string
		value     string
		wantQuote byte
		wantML    bool
	}{
		{name: "empty", value: ``, wantQuote: 0, wantML: false},
		{name: "unquoted", value: `plain`, wantQuote: 0, wantML: false},
		{name: "empty double quoted closes", value: `""`, wantQuote: 0, wantML: false},
		{name: "empty single quoted closes", value: `''`, wantQuote: 0, wantML: false},
		{name: "double quoted closes same line", value: `"x"`, wantQuote: 0, wantML: false},
		{name: "single quoted closes same line", value: `'y'`, wantQuote: 0, wantML: false},
		{name: "unclosed double quote", value: `"x`, wantQuote: '"', wantML: true},
		{name: "unclosed single quote", value: `'y`, wantQuote: '\'', wantML: true},
		{name: "leading whitespace then unclosed", value: "  \"x", wantQuote: '"', wantML: true},
		{name: "escaped closing quote stays open", value: `"a\"`, wantQuote: '"', wantML: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, ml := multilineValueQuote(tc.value)
			if q != tc.wantQuote || ml != tc.wantML {
				t.Fatalf("multilineValueQuote(%q) = (%q, %v), want (%q, %v)", tc.value, q, ml, tc.wantQuote, tc.wantML)
			}
		})
	}
}

// TestDotenvKey pins the key/value split: blank and comment lines yield no key,
// an "export " prefix (space- or tab-separated) is stripped but "exportFOO" is
// a normal key, a key must precede the first '=', and keys with embedded
// whitespace are rejected. The value keeps everything after the first '='.
func TestDotenvKey(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{name: "blank", line: "", wantOK: false},
		{name: "whitespace only", line: "   ", wantOK: false},
		{name: "comment", line: "# x", wantOK: false},
		{name: "comment with equals", line: "#a=b", wantOK: false},
		{name: "simple", line: "FOO=bar", wantKey: "FOO", wantValue: "bar", wantOK: true},
		{name: "single char key", line: "A=b", wantKey: "A", wantValue: "b", wantOK: true},
		{name: "value keeps later equals", line: "FOO=a=b", wantKey: "FOO", wantValue: "a=b", wantOK: true},
		{name: "empty value", line: "FOO=", wantKey: "FOO", wantValue: "", wantOK: true},
		{name: "export space separated", line: "export FOO=bar", wantKey: "FOO", wantValue: "bar", wantOK: true},
		{name: "export tab separated", line: "export\tFOO=bar", wantKey: "FOO", wantValue: "bar", wantOK: true},
		{name: "export with no separator is a normal key", line: "exportFOO=bar", wantKey: "exportFOO", wantValue: "bar", wantOK: true},
		{name: "export alone is not a key", line: "export", wantOK: false},
		{name: "no equals", line: "FOObar", wantOK: false},
		{name: "equals at start", line: "=bar", wantOK: false},
		{name: "key with embedded space", line: "FO O=bar", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, value, ok := dotenvKey(tc.line)
			if key != tc.wantKey || value != tc.wantValue || ok != tc.wantOK {
				t.Fatalf("dotenvKey(%q) = (%q, %q, %v), want (%q, %q, %v)", tc.line, key, value, ok, tc.wantKey, tc.wantValue, tc.wantOK)
			}
		})
	}
}
