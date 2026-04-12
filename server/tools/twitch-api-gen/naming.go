package main

import "strings"

// initialisms are lowercase tokens that should be fully uppercased when they
// appear as a word in a Go identifier.
var initialisms = map[string]bool{
	"id":   true,
	"url":  true,
	"http": true,
	"api":  true,
	"ip":   true,
	"json": true,
	"xml":  true,
	"ssl":  true,
	"tls":  true,
	"html": true,
	"css":  true,
	"uri":  true,
	"uuid": true,
	"igdb": true,
}

// wordOverrides maps lowercase tokens to a specific MixedCase spelling. Use
// this for compound terms that aren't initialisms — e.g. "eventsub" → "EventSub".
var wordOverrides = map[string]string{
	"eventsub": "EventSub",
}

// renderWord returns the Go-name form of a single snake/kebab-split token.
// `first` indicates it's the first token (camel form lowercases non-initialisms).
func renderWord(w string, first, camel bool) string {
	if w == "" {
		return ""
	}
	if initialisms[w] {
		if first && camel {
			return strings.ToLower(w)
		}
		return strings.ToUpper(w)
	}
	if override, ok := wordOverrides[w]; ok {
		if first && camel {
			return strings.ToLower(override)
		}
		return override
	}
	if first && camel {
		return strings.ToLower(w)
	}
	return titleCase(w)
}

// PascalCase converts snake_case or kebab-case to PascalCase, uppercasing known
// initialisms (e.g. "user_id" → "UserID", "profile_image_url" → "ProfileImageURL")
// and applying word overrides (e.g. "create-eventsub-subscription" → "CreateEventSubSubscription").
func PascalCase(s string) string {
	if s == "" {
		return ""
	}
	parts := splitParts(s)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(renderWord(p, false, false))
	}
	return b.String()
}

// CamelCase is like PascalCase but the first part is lowercased (unless it's an initialism).
// Used for parameter names, not struct names.
func CamelCase(s string) string {
	if s == "" {
		return ""
	}
	parts := splitParts(s)
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(renderWord(p, i == 0, true))
	}
	return b.String()
}

// MethodName turns an endpoint ID like "get-users" or "create-eventsub-subscription"
// into a Go method name.
func MethodName(endpointID string) string {
	return PascalCase(endpointID)
}

// splitParts splits on '_' and '-'.
func splitParts(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = toUpperRune(r[0])
	return string(r)
}

func toUpperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}
