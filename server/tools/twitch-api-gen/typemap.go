package main

import (
	"regexp"
	"strconv"
	"strings"
)

// baseTypeMap translates a Twitch-docs-written raw type (left-hand column) into
// a Go type. Object/Object[] map to empty strings because the caller must
// substitute the name of a nested struct.
var baseTypeMap = map[string]string{
	"String":             "string",
	"String[]":           "[]string",
	"Integer":            "int",
	"Unsigned Integer":   "int",
	"Int64":              "int64",
	"Float":              "float64",
	"float":              "float64",
	"Boolean":            "bool",
	"Bool":               "bool",
	"Object":             "", // caller substitutes named struct
	"Object[]":           "",
	"map[string,string]": "map[string]string",
	"map[string]Object":  "map[string]", // caller completes with value type
}

// GoType returns the Go type for a field, given the base primitive mapping.
// For Object / Object[] / map[string]Object the caller must supply nestedName.
// Timestamp and nullable conventions are applied here.
func GoType(f FieldSchema, nestedName string) string {
	base := translate(f.Type, nestedName)

	// Timestamp detection: RFC3339 string → time.Time.
	if f.Type == "String" && (strings.HasSuffix(f.Name, "_at") || strings.Contains(f.Description, "RFC3339")) {
		base = "time.Time"
	}

	// Nullable detection: description contains "**null**" → pointer type.
	if strings.Contains(strings.ToLower(f.Description), "**null**") && base != "" && !strings.HasPrefix(base, "*") && !strings.HasPrefix(base, "[]") {
		base = "*" + base
	}
	return base
}

func translate(rawType, nestedName string) string {
	switch rawType {
	case "Object":
		if nestedName == "" {
			return "any"
		}
		return nestedName
	case "Object[]":
		if nestedName == "" {
			return "[]any"
		}
		return "[]" + nestedName
	case "map[string]Object":
		if nestedName == "" {
			return "map[string]any"
		}
		return "map[string]" + nestedName
	}
	if t, ok := baseTypeMap[rawType]; ok && t != "" {
		return t
	}
	if t, ok := baseTypeMap[rawType]; ok && t == "" {
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
	"get-users":                    "User",
	"get-channel-information":      "ChannelInformation",
	"get-games":                    "Game",
	"get-top-games":                "Game",
	"get-streams":                  "Stream",
	"get-followed-streams":         "Stream",
	"get-videos":                   "Video",
	"get-followed-channels":        "FollowedChannel",
	"create-eventsub-subscription": "EventSubSubscription",
	"get-eventsub-subscriptions":   "EventSubSubscription",
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

