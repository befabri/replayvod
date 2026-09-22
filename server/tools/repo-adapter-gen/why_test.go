package main

import (
	"strings"
	"testing"
)

func TestReasons(t *testing.T) {
	r := testRenderer()
	r.queries["Count"] = querySig{results: []string{"int64", "error"}}
	exec := ctxSig([]string{"error"}, param{"id", "string"}, param{"note", "string"})
	for name, tc := range map[string]struct {
		method string
		sig    methodSig
		hand   string
		want   string
	}{
		"denied":       {"WithTx", ctxSig([]string{"error"}), "", "denied: "},
		"no context":   {"UpdateThing", methodSig{results: []string{"error"}}, "", "no context parameter"},
		"no query":     {"Missing", exec, "", "no sqlc query named Missing"},
		"arguments":    {"UpdateThing", ctxSig([]string{"error"}, param{"id", "string"}, param{"reason", "string"}), "", "arguments do not map onto UpdateThingParams{ID string, Note *string}"},
		"result shape": {"Count", ctxSig([]string{"<unsupported:*ast.MapType>", "error"}), "", "no shape returns (<unsupported:*ast.MapType>, error) from (int64, error)"},
		"body differs": {"UpdateThing", exec, `func (a *PGAdapter) UpdateThing(ctx context.Context, id, note string) error {
	if id == "" {
		return nil
	}
	return a.queries.UpdateThing(ctx, pggen.UpdateThingParams{ID: id, Note: &note})
}
`, "body differs from every generated shape"},
		"matches": {"UpdateThing", exec, `func (a *PGAdapter) UpdateThing(ctx context.Context, id, note string) error {
	return a.queries.UpdateThing(ctx, pggen.UpdateThingParams{ID: id, Note: &note})
}
`, "matches a shape"},
	} {
		norm := ""
		if tc.hand != "" {
			norm = mustNormalize(t, tc.hand)
		}
		got, err := r.reason(tc.method, tc.sig, norm)
		if err != nil || !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: reason = %q, %v; want prefix %q", name, got, err, tc.want)
		}
	}
}
