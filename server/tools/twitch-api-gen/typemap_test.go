package main

import (
	"sort"
	"strings"
	"testing"
)

func TestGoType(t *testing.T) {
	cases := []struct {
		name string
		f    FieldSchema
		nest string
		want string
	}{
		{"string", FieldSchema{Type: "String"}, "", "string"},
		{"string[]", FieldSchema{Type: "String[]"}, "", "[]string"},
		{"int", FieldSchema{Type: "Integer"}, "", "int"},
		{"unsigned int", FieldSchema{Type: "Unsigned Integer"}, "", "int"},
		{"int64", FieldSchema{Type: "Int64"}, "", "int64"},
		{"float", FieldSchema{Type: "Float"}, "", "float64"},
		{"bool", FieldSchema{Type: "Boolean"}, "", "bool"},
		{"object named", FieldSchema{Type: "Object"}, "User", "User"},
		{"object anon", FieldSchema{Type: "Object"}, "", "any"},
		{"object[] named", FieldSchema{Type: "Object[]"}, "User", "[]User"},
		{"map<string,string>", FieldSchema{Type: "map[string,string]"}, "", "map[string]string"},
		{"map<string,object>", FieldSchema{Type: "map[string]Object"}, "Metadata", "map[string]Metadata"},
		{"timestamp by name", FieldSchema{Type: "String", Name: "created_at"}, "", "time.Time"},
		{"timestamp by desc", FieldSchema{Type: "String", Description: "An RFC3339 time"}, "", "time.Time"},
		{"nullable", FieldSchema{Type: "String", Description: "Can be **null**"}, "", "*string"},
		// Lowercase variants (EventSub docs). Parser must be case-insensitive.
		{"lowercase string", FieldSchema{Type: "string"}, "", "string"},
		{"lowercase integer", FieldSchema{Type: "integer"}, "", "int"},
		{"lowercase boolean", FieldSchema{Type: "boolean"}, "", "bool"},
		{"lowercase object named", FieldSchema{Type: "object"}, "User", "User"},
		{"lowercase object[] named", FieldSchema{Type: "object[]"}, "Segment", "[]Segment"},
		{"lowercase timestamp", FieldSchema{Type: "string", Name: "created_at"}, "", "time.Time"},
		{"unix epoch _at string", FieldSchema{Type: "String", Name: "created_at", Description: "Unix timestamp in seconds."}, "", "string"},
	}
	for _, c := range cases {
		got := GoType(c.f, c.nest)
		if got != c.want {
			t.Errorf("%s: GoType(%+v, %q) = %q; want %q", c.name, c.f, c.nest, got, c.want)
		}
	}
}

func TestToParamFieldModel_usesExplicitRepeatedQueryParamMap(t *testing.T) {
	f := FieldSchema{
		Name: "segment",
		Type: "String",
		Description: "You may specify one or more segments. To specify multiple segments, " +
			"include the segment parameter for each segment to get.",
	}
	if IsArrayParam(f.Description) {
		t.Fatalf("test setup should exercise the explicit map, not the prose matcher")
	}

	got, err := toParamFieldModel("get-extension-configuration-segment", f, silentLogger())
	if err != nil {
		t.Fatalf("toParamFieldModel: %v", err)
	}
	if got.GoType != "[]string" {
		t.Errorf("GoType = %q; want []string for explicit repeated query parameter", got.GoType)
	}
	if got.OmitEmpty {
		t.Errorf("OmitEmpty = true; want false for repeated query parameters")
	}
}

func TestRepeatedQueryParams_contractMatchesReferenceSnapshot(t *testing.T) {
	ids := repeatedQueryParamEndpointIDs()
	defs := parseSnapshotEndpointDefs(t, ids)

	byID := make(map[string]EndpointDef, len(defs))
	for _, ep := range defs {
		byID[ep.ID] = ep
	}

	for _, endpointID := range ids {
		ep, ok := byID[endpointID]
		if !ok {
			t.Errorf("repeatedQueryParams references missing endpoint %q", endpointID)
			continue
		}

		queryFields := make(map[string]FieldSchema, len(ep.QueryParams))
		for _, field := range ep.QueryParams {
			queryFields[field.Name] = field
		}

		fieldNames := repeatedQueryParamFieldNames(endpointID)
		for _, fieldName := range fieldNames {
			field, ok := queryFields[fieldName]
			if !ok {
				t.Errorf("repeatedQueryParams references missing field %q on endpoint %q", fieldName, endpointID)
				continue
			}

			model, err := toParamFieldModel(endpointID, field, silentLogger())
			if err != nil {
				t.Errorf("toParamFieldModel(%q, %q): %v", endpointID, fieldName, err)
				continue
			}
			if !strings.HasPrefix(model.GoType, "[]") {
				t.Errorf("%s.%s GoType = %q; want slice type", endpointID, fieldName, model.GoType)
			}
			if model.OmitEmpty {
				t.Errorf("%s.%s OmitEmpty = true; repeated query params must omit omitempty", endpointID, fieldName)
			}
			if !IsRepeatedQueryParam(endpointID, fieldName) {
				t.Errorf("IsRepeatedQueryParam(%q, %q) = false; want true", endpointID, fieldName)
			}
		}
	}
}

func TestRepeatedQueryParams_suspiciousDocsWordingRequiresExplicitMapping(t *testing.T) {
	ids := append([]string{}, endpoints...)
	ids = append(ids, repeatedQueryParamEndpointIDs()...)
	defs := parseSnapshotEndpointDefs(t, dedupeStrings(ids))

	for _, ep := range defs {
		for _, field := range ep.QueryParams {
			if !IsArrayParam(field.Description) {
				continue
			}
			if IsRepeatedQueryParam(ep.ID, field.Name) {
				continue
			}
			t.Errorf("%s.%s has repeated-query wording but is missing from repeatedQueryParams: %q", ep.ID, field.Name, field.Description)
		}
	}
}

func repeatedQueryParamEndpointIDs() []string {
	ids := make([]string, 0, len(repeatedQueryParams))
	for endpointID := range repeatedQueryParams {
		ids = append(ids, endpointID)
	}
	sort.Strings(ids)
	return ids
}

func repeatedQueryParamFieldNames(endpointID string) []string {
	fields := make([]string, 0, len(repeatedQueryParams[endpointID]))
	for fieldName := range repeatedQueryParams[endpointID] {
		fields = append(fields, fieldName)
	}
	sort.Strings(fields)
	return fields
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
