package main

import (
	"regexp"
	"strconv"
	"strings"
)

// baseTypeMap translates a Twitch-docs-written raw type (left-hand column) into
// a Go type. Object/Object[] map to empty strings because the caller must
// substitute the name of a nested struct.
//
// Keys are lowercased for case-insensitive lookup: Helix docs use "String"/"Yes",
// EventSub docs use "string"/"yes". lookupType normalizes the input before lookup.
var baseTypeMap = map[string]string{
	"string":             "string",
	"string[]":           "[]string",
	"[]string":           "[]string", // EventSub docs sometimes use this form
	"integer":            "int",
	"int":                "int", // EventSub shorthand
	"int[]":              "[]int",
	"unsigned integer":   "int",
	"int64":              "int64",
	"float":              "float64",
	"boolean":            "bool",
	"bool":               "bool",
	"object":             "", // caller substitutes named struct
	"object[]":           "",
	"map[string,string]": "map[string]string",
	"map[string]object":  "map[string]", // caller completes with value type
}

func lookupType(rawType string) (string, bool) {
	t, ok := baseTypeMap[strings.ToLower(rawType)]
	return t, ok
}

func isObjectType(rawType string) bool {
	switch strings.ToLower(rawType) {
	case "object":
		return true
	}
	return false
}

func isObjectArrayType(rawType string) bool {
	return strings.EqualFold(rawType, "object[]")
}

func isMapObjectType(rawType string) bool {
	return strings.EqualFold(rawType, "map[string]object")
}

func isStringType(rawType string) bool {
	return strings.EqualFold(rawType, "string")
}

// GoType returns the Go type for a field, given the base primitive mapping.
// For Object / Object[] / map[string]Object the caller must supply nestedName.
// Timestamp and nullable conventions are applied here.
func GoType(f FieldSchema, nestedName string) string {
	base := translate(f.Type, nestedName)

	// Timestamp detection: RFC3339 string → time.Time.
	if isStringType(f.Type) && isTimestampString(f) {
		base = "time.Time"
	}

	// Nullable detection: description contains "**null**" → pointer type.
	if strings.Contains(strings.ToLower(f.Description), "**null**") && base != "" && !strings.HasPrefix(base, "*") && !strings.HasPrefix(base, "[]") {
		base = "*" + base
	}
	return base
}

func isTimestampString(f FieldSchema) bool {
	desc := strings.ToLower(f.Description)
	if strings.Contains(desc, "unix") || strings.Contains(desc, "epoch") {
		return false
	}
	return strings.HasSuffix(f.Name, "_at") || strings.Contains(f.Description, "RFC3339")
}

func translate(rawType, nestedName string) string {
	switch {
	case isObjectType(rawType):
		if nestedName == "" {
			return "any"
		}
		return nestedName
	case isObjectArrayType(rawType):
		if nestedName == "" {
			return "[]any"
		}
		return "[]" + nestedName
	case isMapObjectType(rawType):
		if nestedName == "" {
			return "map[string]any"
		}
		return "map[string]" + nestedName
	}
	if t, ok := lookupType(rawType); ok && t != "" {
		return t
	}
	if _, ok := lookupType(rawType); ok {
		return "any"
	}
	// Unknown type: fall back to any, caller should log.
	return "any"
}

// responseBodySchemaNames maps endpoint IDs to the struct name to use for the
// `data[]` element of the response (or, for schedule endpoints, `data.segments[]`).
// Ported selectively from RESPONSE_BODY_SCHEMA_NAMES in constants.ts —
// only entries relevant to Phase 3/4/5 are included.
var responseBodySchemaNames = map[string]string{
	"get-users":                              "User",
	"get-channel-information":                "ChannelInformation",
	"get-games":                              "Game",
	"get-top-games":                          "Game",
	"get-streams":                            "Stream",
	"get-followed-streams":                   "Stream",
	"get-videos":                             "Video",
	"get-followed-channels":                  "FollowedChannel",
	"create-eventsub-subscription":           "EventSubSubscription",
	"get-eventsub-subscriptions":             "EventSubSubscription",
	"get-clips":                              "Clip",
	"create-stream-marker":                   "StreamMarkerCreated",
	"get-stream-markers":                     "StreamMarkers",
	"get-channel-stream-schedule":            "ChannelStreamSchedule",
	"create-channel-stream-schedule-segment": "ChannelStreamSchedule",
	"update-channel-stream-schedule-segment": "ChannelStreamSchedule",
}

// responseItemType returns the struct name for the `data[]` element of an endpoint,
// or an empty string if no explicit mapping exists.
func responseItemType(endpointID string) string {
	return responseBodySchemaNames[endpointID]
}

// arrayParamRegexes match query-parameter descriptions that signal a repeated
// parameter like `?id=A&id=B`. Ported from parseSchemaObject.ts.
var arrayParamRegexes = []*regexp.Regexp{
	regexp.MustCompile(`You may specify a maximum of (\d+)`),
	regexp.MustCompile(`up to a maximum of (\d+)`),
	regexp.MustCompile(`To specify more than one`),
}

// IsArrayParam reports whether a query-parameter description signals an
// array-of-values parameter (Twitch's convention: `?id=A&id=B`).
func IsArrayParam(description string) bool {
	for _, re := range arrayParamRegexes {
		m := re.FindStringSubmatch(description)
		if m == nil {
			continue
		}
		if len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil && n <= 1 {
				continue
			}
		}
		return true
	}
	return false
}

// repeatedQueryParams is the explicit source of truth for Helix query
// parameters that should be encoded as repeated values (`?id=A&id=B`). The old
// prose matcher remains useful for endpoint-agnostic constraint extraction, but
// generated API shape should not depend on Twitch's exact wording.
var repeatedQueryParams = map[string]map[string]bool{
	// ?broadcaster_id=A&broadcaster_id=B, max 100.
	"get-channel-information": {
		"broadcaster_id": true,
	},
	// ?id=A&id=B, max 100 schedule segments.
	"get-channel-stream-schedule": {
		"id": true,
	},
	// ?id=A&id=B, max 100 clips.
	"get-clips": {
		"id": true,
	},
	// ?segment=viewer&segment=broadcaster; docs say include segment per segment.
	"get-extension-configuration-segment": {
		"segment": true,
	},
	// ?id=A&id=B / ?name=A&name=B / ?igdb_id=A&igdb_id=B, max 100 for IDs.
	"get-games": {
		"id":      true,
		"name":    true,
		"igdb_id": true,
	},
	// ?user_id=A&user_id=B etc.; docs allow multiple filters, IDs max 100.
	"get-streams": {
		"user_id":    true,
		"user_login": true,
		"game_id":    true,
		"language":   true,
	},
	// ?id=A&id=B / ?login=A&login=B, max 100.
	"get-users": {
		"id":    true,
		"login": true,
	},
	// ?id=A&id=B, max 100 videos.
	"get-videos": {
		"id": true,
	},
}

func IsRepeatedQueryParam(endpointID, name string) bool {
	return repeatedQueryParams[endpointID][name]
}
