package main

import (
	"bytes"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExplainDiff exercises each output branch of explainDiff so a future
// refactor that silently mis-summarises diffs fails at CI time, not at review
// time when someone trusts the wrong summary.
func TestExplainDiff(t *testing.T) {
	const header = "package twitch\n\n"
	// Base file shared by most cases.
	base := header + `
type A struct {
	X string ` + "`validate:\"required\"`" + `
}
type B struct {
	Y int
}
`
	cases := []struct {
		name        string
		old         string
		new         string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "identical files",
			old:         base,
			new:         base,
			wantContain: []string{"no type-level changes"},
		},
		{
			name: "added type",
			old:  base,
			new: base + `
type C struct {
	Z string
}
`,
			wantContain: []string{"Added types (1)", "+ C (1 fields)"},
		},
		{
			name: "removed type",
			old:  base,
			new: header + `
type A struct {
	X string ` + "`validate:\"required\"`" + `
}
`,
			wantContain: []string{"Removed types (1)", "- B"},
		},
		{
			name: "added field",
			old:  base,
			new: header + `
type A struct {
	X string ` + "`validate:\"required\"`" + `
	Extra bool
}
type B struct {
	Y int
}
`,
			wantContain: []string{"Changed types (1)", "A:", "+ Extra bool"},
		},
		{
			name: "changed field type",
			old:  base,
			new: header + `
type A struct {
	X int ` + "`validate:\"required\"`" + `
}
type B struct {
	Y int
}
`,
			wantContain: []string{"Changed types (1)", "A:", "~ X: type string -> int"},
		},
		{
			name: "changed validate tag",
			old:  base,
			new: header + `
type A struct {
	X string ` + "`validate:\"required,max=10\"`" + `
}
type B struct {
	Y int
}
`,
			wantContain: []string{"Changed types (1)", "A:", `~ X: validate "required" -> "required,max=10"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldPath := writeTempGo(t, tc.old)
			newPath := writeTempGo(t, tc.new)
			var buf bytes.Buffer
			if err := explainDiff(oldPath, newPath, &buf); err != nil {
				t.Fatalf("explainDiff: %v", err)
			}
			out := buf.String()
			for _, want := range tc.wantContain {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
			for _, unwanted := range tc.wantAbsent {
				if strings.Contains(out, unwanted) {
					t.Errorf("output unexpectedly contains %q:\n%s", unwanted, out)
				}
			}
		})
	}
}

// TestExprString_unknownFallthrough guards that exprString's fallback names
// the concrete ast node type in the sentinel so a new unhandled shape shows
// up visibly in -explain output instead of silently equating two types.
func TestExprString_unknownFallthrough(t *testing.T) {
	got := exprString(&ast.FuncType{})
	if !strings.Contains(got, "FuncType") {
		t.Errorf("exprString on FuncType = %q; want sentinel naming FuncType", got)
	}
}

func writeTempGo(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "src.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}
