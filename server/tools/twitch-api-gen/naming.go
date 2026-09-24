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
func renderWord(w string) string {
	if w == "" {
		return ""
	}
	if initialisms[w] {
		return strings.ToUpper(w)
	}
	if override, ok := wordOverrides[w]; ok {
		return override
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
		b.WriteString(renderWord(p))
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
