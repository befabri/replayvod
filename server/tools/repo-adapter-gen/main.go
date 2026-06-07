// Command repo-adapter-gen generates the mechanical row->domain mapper functions
// for the Postgres and SQLite repository adapters from a single source.
//
// It parses the domain model structs (internal/repository/models.go) and the
// two sqlc-generated row structs (pggen, sqlitegen), matches their fields by
// name, and emits pg<Type>ToDomain / sqlite<Type>ToDomain using a type-driven
// conversion table. Any field whose (rowType -> domainType) conversion is not
// in the table is a hard error: such tables stay hand-written. This keeps the
// generator scoped to the boring, identical ~80% and never silently guesses.
//
// The repository contract test (internal/repository/contracttest) is the
// acceptance gate: generated adapters must pass it on both backends unchanged.
//
// Usage:
//
//	go run ./tools/repo-adapter-gen            # write the generated files
//	go run ./tools/repo-adapter-gen -check     # fail if generated files are stale
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// genSpec names one generated mapper. name is the function suffix
// (pg<name>ToDomain). domain/row override the domain struct and sqlc row struct
// names when they differ from name (e.g. domain EventSubSnapshot from sqlc row
// EventsubSnapshot, exposed as Snapshot). Empty domain/row default to name.
type genSpec struct {
	name   string
	domain string
	row    string
	// slice also emits pg<name>sToDomain([]row) []domain, which calls the
	// single-row mapper. Only set it when the plural is name+"s" and the row
	// type is the plain singular row (true for the simple list-of-rows mappers).
	slice bool
}

func (s genSpec) domainType() string {
	if s.domain != "" {
		return s.domain
	}
	return s.name
}

func (s genSpec) rowType() string {
	if s.row != "" {
		return s.row
	}
	return s.name
}

// genTypes is the allowlist of types whose mappers are generated. A type belongs
// here only if every one of its domain fields maps to a row field with a known
// conversion (see convRules). Complex/non-1:1 types stay hand-written.
var genTypes = []genSpec{
	{name: "Title"},
	{name: "Tag"},
	{name: "Category"},
	{name: "Channel"},
	{name: "ChannelUserState"},
	{name: "EventLog", slice: true},
	{name: "Job"},
	{name: "RecordingWebhookDelivery"},
	{name: "Stream", slice: true},
	{name: "Subscription", slice: true},
	{name: "Task", slice: true},
	{name: "User"},
	{name: "VideoPart"},
	{name: "VideoPlaybackAsset"},
	{name: "VideoUserState"},
	{name: "WebhookEvent", slice: true},
	{name: "ServerSettings", row: "ServerSetting"},
	{name: "Settings", row: "Setting"},
	{name: "Snapshot", domain: "EventSubSnapshot", row: "EventsubSnapshot"},
}

// convRules maps {sqlcRowFieldType, domainFieldType} to a Go expression template
// where %s is the source selector (e.g. "src.CreatedAt"). PG rows are overridden
// in sqlc.yaml to already match the domain types, so most PG conversions are
// identity; SQLite carries the type glue.
var convRules = map[[2]string]string{
	// identity (same type both sides)
	{"int64", "int64"}:                     "%s",
	{"string", "string"}:                   "%s",
	{"bool", "bool"}:                       "%s",
	{"*string", "*string"}:                 "%s",
	{"*int64", "*int64"}:                   "%s",
	{"time.Time", "time.Time"}:             "%s",
	{"*time.Time", "*time.Time"}:           "%s",
	{"json.RawMessage", "json.RawMessage"}: "%s",
	// SQLite glue
	{"sqlitetype.Time", "time.Time"}:   "%s.Time",
	{"*sqlitetype.Time", "*time.Time"}: "timePtrFromSQLite(%s)",
	{"int64", "bool"}:                  "%s != 0",
	{"sql.NullInt64", "*int64"}:        "int64PtrFromSQLite(%s)",
	// numeric width/alias conversions (PG int4 -> int32, SQLite INTEGER -> int64)
	{"int", "int"}:         "%s",
	{"int32", "int32"}:     "%s",
	{"int32", "int"}:       "int(%s)",
	{"int64", "int"}:       "int(%s)",
	{"int32", "int64"}:     "int64(%s)",
	{"int64", "int32"}:     "int32(%s)",
	{"float64", "float64"}: "%s",
	// PG nullable identity (sqlc.yaml overrides nullable cols to pointers)
	{"*bool", "*bool"}:       "%s",
	{"*float64", "*float64"}: "%s",
	{"*int32", "*int32"}:     "%s",
	// SQLite nullable scalars via the adapter's existing helpers
	{"sql.NullString", "*string"}:         "fromNullString(%s)",
	{"sql.NullInt64", "*bool"}:            "nullInt64ToBool(%s)",
	{"sql.NullFloat64", "*float64"}:       "fromNullFloat64(%s)",
	{"sql.NullString", "json.RawMessage"}: "rawMessageFromSQLite(%s)",
	{"string", "json.RawMessage"}:         "json.RawMessage(%s)",
}

type dialect struct {
	name     string // "pg" / "sqlite"
	dir      string // adapter package dir
	genPkg   string // "pggen" / "sqlitegen"
	genAlias string // import path of the gen package
}

func main() {
	root := flag.String("root", ".", "server module root")
	check := flag.Bool("check", false, "verify generated files are up to date instead of writing")
	flag.Parse()

	domain, err := structFields(filepath.Join(*root, "internal/repository/models.go"))
	if err != nil {
		fail(err)
	}

	dialects := []dialect{
		{name: "pg", dir: "internal/repository/pgadapter", genPkg: "pggen",
			genAlias: "github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"},
		{name: "sqlite", dir: "internal/repository/sqliteadapter", genPkg: "sqlitegen",
			genAlias: "github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"},
	}

	for _, d := range dialects {
		rows, err := structFields(filepath.Join(*root, d.dir, d.genPkg, "models.go"))
		if err != nil {
			fail(err)
		}
		src, err := generate(d, domain, rows)
		if err != nil {
			fail(fmt.Errorf("%s: %w", d.name, err))
		}
		out := filepath.Join(*root, d.dir, "mappers_gen.go")
		if *check {
			existing, _ := os.ReadFile(out)
			if !bytes.Equal(existing, src) {
				fail(fmt.Errorf("%s is stale; run: go run ./tools/repo-adapter-gen", out))
			}
			continue
		}
		if err := os.WriteFile(out, src, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s (%d types)\n", out, len(genTypes))
	}
}

// generate renders the mappers_gen.go body for one dialect.
func generate(d dialect, domain, rows map[string]map[string]string) ([]byte, error) {
	pkgName := filepath.Base(d.dir)
	var body strings.Builder

	for _, spec := range genTypes {
		typ := spec.name
		domFields, ok := domain[spec.domainType()]
		if !ok {
			return nil, fmt.Errorf("domain type %q not found", spec.domainType())
		}
		rowFields, ok := rows[spec.rowType()]
		if !ok {
			return nil, fmt.Errorf("%s row type %q not found", d.genPkg, spec.rowType())
		}
		fmt.Fprintf(&body, "\nfunc %s%sToDomain(src %s.%s) *repository.%s {\n\treturn &repository.%s{\n",
			d.name, spec.name, d.genPkg, spec.rowType(), spec.domainType(), spec.domainType())
		// Row fields keyed by lowercased name so we can match across the
		// initialism-casing differences between sqlc (BoxArtUrl, IgdbID) and the
		// domain structs (BoxArtURL, IGDBID).
		rowByNorm := make(map[string]string, len(rowFields))
		for rf := range rowFields {
			rowByNorm[strings.ToLower(rf)] = rf
		}
		// Deterministic field order.
		names := make([]string, 0, len(domFields))
		for f := range domFields {
			names = append(names, f)
		}
		sort.Strings(names)
		for _, f := range names {
			domType := domFields[f]
			rowName, ok := rowByNorm[strings.ToLower(f)]
			if !ok {
				return nil, fmt.Errorf("type %s: domain field %q has no row field (not a 1:1 table — hand-write it)", typ, f)
			}
			rowType := rowFields[rowName]
			tmpl, ok := convRules[[2]string{rowType, domType}]
			if !ok {
				return nil, fmt.Errorf("type %s field %q: no conversion rule for row %q -> domain %q", typ, f, rowType, domType)
			}
			expr := fmt.Sprintf(tmpl, "src."+rowName)
			fmt.Fprintf(&body, "\t\t%s: %s,\n", f, expr)
		}
		body.WriteString("\t}\n}\n")

		if spec.slice {
			fmt.Fprintf(&body, "\nfunc %s%ssToDomain(rows []%s.%s) []repository.%s {\n",
				d.name, spec.name, d.genPkg, spec.rowType(), spec.domainType())
			fmt.Fprintf(&body, "\tout := make([]repository.%s, len(rows))\n", spec.domainType())
			fmt.Fprintf(&body, "\tfor i, r := range rows {\n\t\tout[i] = *%s%sToDomain(r)\n\t}\n\treturn out\n}\n",
				d.name, spec.name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by repo-adapter-gen. DO NOT EDIT.\n\npackage %s\n\n", pkgName)
	b.WriteString("import (\n")
	fmt.Fprintf(&b, "\t%q\n", "github.com/befabri/replayvod/server/internal/repository")
	if strings.Contains(body.String(), "json.") {
		fmt.Fprintf(&b, "\t%q\n", "encoding/json")
	}
	fmt.Fprintf(&b, "\t%q\n", d.genAlias)
	b.WriteString(")\n")
	b.WriteString(body.String())

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w\n%s", err, b.String())
	}
	return formatted, nil
}

// structFields parses a Go file and returns, per struct type name, a map of
// field name -> rendered field type.
func structFields(path string) (map[string]map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fields := map[string]string{}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					if !name.IsExported() {
						continue
					}
					fields[name.Name] = renderType(field.Type)
				}
			}
			out[ts.Name.Name] = fields
		}
	}
	return out, nil
}

// renderType renders the type expressions the model files actually use:
// identifiers, pointers, selectors (pkg.Type), and []byte.
func renderType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + renderType(t.X)
	case *ast.SelectorExpr:
		return renderType(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + renderType(t.Elt)
		}
	}
	return fmt.Sprintf("<unsupported:%T>", e)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "repo-adapter-gen:", err)
	os.Exit(1)
}
