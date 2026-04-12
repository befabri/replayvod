package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Constraint extractor for go-playground/validator tags.
//
// Philosophy: under-constrained is safe, over-constrained breaks legitimate
// requests. Each regex here targets a phrasing with near-zero false-positive
// risk. When Twitch's wording varies, we'd rather miss the constraint (and
// fall back to Twitch's 400 response) than emit a rule that rejects valid
// input client-side.
//
// To add a new regex, find a real phrasing in the snapshot HTML, add a
// high-confidence pattern, add positive + negative cases to constraints_test.go,
// regenerate, and review the diff.
//
// See the plan in .docs/plans/ for scope/timing notes.

var (
	// "maximum of 140 characters", "maximum length is 140 characters", etc.
	// Requires the literal word "character(s)" at the end to avoid hitting
	// phrases like "maximum is 100" that could refer to counts, IDs, ranges…
	reStringMax = regexp.MustCompile(`(?i)maximum\s+(?:length\s+)?(?:of|is)\s+(\d+)\s+characters?`)

	// "Between 1 and 140 characters long", "from 1 to 140 characters".
	reStringRange = regexp.MustCompile(`(?i)(?:between|from)\s+(\d+)\s+(?:and|to)\s+(\d+)\s+characters?`)

	// Explicit "Range: 0 - 900" — only numeric ranges that Twitch spells out
	// with the literal "Range" keyword. Covers `delay`, `slow_mode_wait_time`,
	// etc. Intentionally avoids free-form "minimum is 1 and maximum is 100"
	// spread across sentences — too brittle.
	reNumericRange = regexp.MustCompile(`(?i)range[:\s]+(\d+)\s*[-\x{2013}\x{2014}]\s*(\d+)`)

	// "maximum of 100 IDs", "maximum of 10 tags". Lists specific plural nouns
	// Twitch uses so we don't accidentally capture "characters".
	reArrayMax = regexp.MustCompile(`(?i)maximum\s+of\s+(\d+)\s+(?:ids?|tags?|items?|urls?|redemptions?|broadcasters?)`)

	// "Must be a positive integer." Pure signal for min=1 on numeric fields.
	rePositiveInt = regexp.MustCompile(`(?i)\bmust\s+be\s+a\s+positive\s+integer\b`)
)

// ExtractConstraints inspects a parsed FieldSchema and returns two validator-tag
// fragments: `base` applies to the field itself and `dive` applies to each
// element of an array-typed field. Either (or both) can be empty.
//
// Callers combine them in the template as:
//
//	validate:"<required|omitempty>,<base>[,dive,<dive>]"
//
// Only request-side types (Params, Body) should render these tags — response
// types are parsed, not validated.
func ExtractConstraints(f FieldSchema) (base, dive string) {
	desc := f.Description
	isArrayField := strings.HasSuffix(f.Type, "[]") || IsArrayParam(desc)

	var baseParts, diveParts []string

	// String max: applies to element-level for arrays, to the field for scalars.
	if m := reStringMax.FindStringSubmatch(desc); m != nil {
		if isArrayField {
			diveParts = append(diveParts, "max="+m[1])
		} else {
			baseParts = append(baseParts, "max="+m[1])
		}
	}

	// String range (min + max together): same routing as max.
	if m := reStringRange.FindStringSubmatch(desc); m != nil {
		if isArrayField {
			diveParts = append(diveParts, "min="+m[1], "max="+m[2])
		} else {
			baseParts = append(baseParts, "min="+m[1], "max="+m[2])
		}
	}

	// Explicit numeric range — always applies to the field itself.
	if m := reNumericRange.FindStringSubmatch(desc); m != nil {
		baseParts = append(baseParts, "min="+m[1], "max="+m[2])
	}

	// Array max ("maximum of 100 IDs") — array-level, not element-level.
	if m := reArrayMax.FindStringSubmatch(desc); m != nil && isArrayField {
		baseParts = append(baseParts, "max="+m[1])
	}

	// Positive-integer assertion — only meaningful on numeric types.
	if rePositiveInt.MatchString(desc) && isNumericType(f.Type) {
		baseParts = append(baseParts, "min=1")
	}

	// Enums from the parser. For string-array fields the oneof lives in dive;
	// for scalars it lives in base.
	if len(f.EnumValues) > 0 {
		enum := formatEnumOneof(f.EnumValues)
		if enum != "" {
			if isArrayField {
				diveParts = append(diveParts, enum)
			} else {
				baseParts = append(baseParts, enum)
			}
		}
	}

	return strings.Join(dedupe(baseParts), ","), strings.Join(dedupe(diveParts), ",")
}

// formatEnumOneof turns the parsed EnumValues into a go-playground/validator
// `oneof=A B C` segment. Values containing spaces are skipped (go-playground
// uses space as the separator); the set is usually simple enough that no
// escaping is needed in practice.
func formatEnumOneof(values []any) string {
	var out []string
	for _, v := range values {
		s := fmt.Sprintf("%v", v)
		if s == "" || strings.ContainsAny(s, " \t") {
			return "" // bail on spaces — don't emit a broken tag
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return ""
	}
	return "oneof=" + strings.Join(out, " ")
}

// isNumericType reports whether the Twitch-written type is integer-or-float-like.
func isNumericType(t string) bool {
	switch t {
	case "Integer", "Unsigned Integer", "Int64", "Float", "float":
		return true
	}
	return false
}

// dedupe preserves order and drops exact duplicates.
func dedupe(parts []string) []string {
	if len(parts) < 2 {
		return parts
	}
	seen := make(map[string]bool, len(parts))
	out := parts[:0]
	for _, p := range parts {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
