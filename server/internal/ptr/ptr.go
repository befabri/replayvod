// Package ptr holds small generic helpers for converting primitive
// zero-values into nullable pointers when crossing an external/internal
// boundary. Centralized so the Twitch-response → domain translations in
// each domain service don't re-derive the same 4-line helper.
package ptr

// StringOrNil returns nil for the empty string and &s otherwise.
// Twitch Helix returns "" for absent optional strings; our domain
// models use *string for nullability. StringOrNil is the idiomatic
// bridge.
func StringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
