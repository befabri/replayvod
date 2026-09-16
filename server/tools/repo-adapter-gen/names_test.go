package main

import (
	"strings"
	"testing"
)

func namesRenderer() renderer {
	return renderer{
		d: dialect{name: "pg", genPkg: "pggen", adapterType: "PGAdapter"},
		gen: map[string]map[string]string{
			"ListThingsParams":    {"AfterID": "string", "Limit": "int32"},
			"RecordThingParams":   {"ID": "int64", "UserID": "string", "ProgressAtMs": "int64"},
			"UpsertThingParams":   {"Name": "string", "Note": "*string"},
			"ListVisibleParams":   {"OwnerID": "string", "Limit": "int32"},
			"SnapshotThingParams": {"ID": "int64", "TakenAt": "*time.Time"},
		},
		queries: map[string]querySig{
			"ListThings":    {params: []param{{"arg", "ListThingsParams"}}, results: []string{"[]Thing", "error"}},
			"RecordThing":   {params: []param{{"arg", "RecordThingParams"}}, results: []string{"error"}},
			"UpsertThing":   {params: []param{{"arg", "UpsertThingParams"}}, results: []string{"error"}},
			"ListVisible":   {params: []param{{"arg", "ListVisibleParams"}}, results: []string{"[]Thing", "error"}},
			"SnapshotThing": {params: []param{{"arg", "SnapshotThingParams"}}, results: []string{"error"}},
			"DeleteThing":   {params: []param{{"id", "string"}}, results: []string{"error"}},
		},
	}
}

func TestMisnamedParams(t *testing.T) {
	r := namesRenderer()
	tests := []struct {
		name   string
		method string
		sig    methodSig
		want   []string
	}{
		{
			name:   "names match case-insensitively",
			method: "ListThings",
			sig:    ctxSig([]string{"[]Thing", "error"}, param{"afterId", "string"}, param{"limit", "int"}),
		},
		{
			name:   "a drifted name is reported with the fields it could have used",
			method: "ListThings",
			sig:    ctxSig([]string{"[]Thing", "error"}, param{"after", "string"}, param{"limit", "int"}),
			want:   []string{"ListThings: parameter after has no field in ListThingsParams (AfterID, Limit)"},
		},
		{
			name:   "a derived value with no conversion to any field is not checked, its siblings are",
			method: "RecordThing",
			sig:    ctxSig([]string{"error"}, param{"userID", "string"}, param{"videoID", "int64"}, param{"at", "time.Time"}),
			want:   []string{"RecordThing: parameter videoID has no field in RecordThingParams (ID, ProgressAtMs, UserID)"},
		},
		{
			name:   "a pointer conversion still counts as reachable",
			method: "SnapshotThing",
			sig:    ctxSig([]string{"error"}, param{"id", "int64"}, param{"at", "time.Time"}),
			want:   []string{"SnapshotThing: parameter at has no field in SnapshotThingParams (ID, TakenAt)"},
		},
		{
			name:   "a domain input struct is destructured by hand",
			method: "UpsertThing",
			sig:    ctxSig([]string{"error"}, param{"input", "*ThingInput"}),
		},
		{
			name:   "a domain value beside scalars still exempts the method",
			method: "ListVisible",
			sig:    ctxSig([]string{"[]Thing", "error"}, param{"opts", "ListOpts"}, param{"limit", "int"}),
		},
		{
			name:   "a positional query has no fields to compare",
			method: "DeleteThing",
			sig:    ctxSig([]string{"error"}, param{"thingID", "string"}),
		},
		{
			name:   "a method without a query is not compared",
			method: "CountThings",
			sig:    ctxSig([]string{"int64", "error"}, param{"ownerID", "string"}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.misnamedParams(tt.method, tt.sig)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("misnamedParams() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestIsScalarType(t *testing.T) {
	for typ, want := range map[string]bool{
		"string": true, "int": true, "*int64": true, "[]string": true, "time.Time": true, "*time.Time": true, "json.RawMessage": true,
		"Thing": false, "*ThingInput": false, "[]Thing": false, "<unsupported:*ast.FuncType>": false, "": false,
	} {
		if got := isScalarType(typ); got != want {
			t.Errorf("isScalarType(%q) = %v, want %v", typ, got, want)
		}
	}
}
