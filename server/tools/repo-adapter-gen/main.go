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
	"unicode"
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
	name        string // "pg" / "sqlite"
	dir         string // adapter package dir
	genPkg      string // "pggen" / "sqlitegen"
	genAlias    string // import path of the gen package
	adapterType string // "PGAdapter" / "SQLiteAdapter"
}

// genMethods is the allowlist of repository.Repository methods whose bodies are
// generated. Restricted to the unambiguous shapes: an exec query taking only ctx
// or scalar args passed positionally, and a one-row query whose scalar args pass
// positionally and whose result maps through a generated single-row mapper. Args
// that need a sqlc params struct, not-found translation, or any logic stay
// hand-written. Signature and classification are inferred from the interface.
var genMethods = []string{
	"DeleteExpiredAppTokens",
	"DeleteExpiredSessions",
	"UpsertTitle",
	"UpsertTag",
}

func main() {
	root := flag.String("root", ".", "server module root")
	check := flag.Bool("check", false, "verify generated files are up to date instead of writing")
	flag.Parse()

	domain, err := structFields(filepath.Join(*root, "internal/repository/models.go"))
	if err != nil {
		fail(err)
	}
	methods, err := interfaceMethods(filepath.Join(*root, "internal/repository/repository.go"), "Repository")
	if err != nil {
		fail(err)
	}

	dialects := []dialect{
		{name: "pg", dir: "internal/repository/pgadapter", genPkg: "pggen", adapterType: "PGAdapter",
			genAlias: "github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"},
		{name: "sqlite", dir: "internal/repository/sqliteadapter", genPkg: "sqlitegen", adapterType: "SQLiteAdapter",
			genAlias: "github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"},
	}

	for _, d := range dialects {
		rows, err := structFields(filepath.Join(*root, d.dir, d.genPkg, "models.go"))
		if err != nil {
			fail(err)
		}
		mapperSrc, err := generate(d, domain, rows)
		if err != nil {
			fail(fmt.Errorf("%s mappers: %w", d.name, err))
		}
		methodSrc, err := generateMethods(d, methods)
		if err != nil {
			fail(fmt.Errorf("%s methods: %w", d.name, err))
		}
		outputs := []struct {
			path string
			src  []byte
			note string
		}{
			{filepath.Join(*root, d.dir, "mappers_gen.go"), mapperSrc, fmt.Sprintf("%d types", len(genTypes))},
			{filepath.Join(*root, d.dir, "methods_gen.go"), methodSrc, fmt.Sprintf("%d methods", len(genMethods))},
		}
		for _, o := range outputs {
			if *check {
				existing, _ := os.ReadFile(o.path)
				if !bytes.Equal(existing, o.src) {
					fail(fmt.Errorf("%s is stale; run: go run ./tools/repo-adapter-gen", o.path))
				}
				continue
			}
			if err := os.WriteFile(o.path, o.src, 0o644); err != nil {
				fail(err)
			}
			fmt.Printf("wrote %s (%s)\n", o.path, o.note)
		}
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

type param struct{ name, typ string }

type methodSig struct {
	params  []param
	results []string
}

// interfaceMethods parses the named interface and returns each method's
// parameter and result types.
func interfaceMethods(path, ifaceName string) (map[string]methodSig, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]methodSig{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != ifaceName {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			for _, m := range it.Methods.List {
				if len(m.Names) == 0 {
					continue // embedded interface
				}
				ft, ok := m.Type.(*ast.FuncType)
				if !ok {
					continue
				}
				var sig methodSig
				if ft.Params != nil {
					for _, p := range ft.Params.List {
						typ := renderType(p.Type)
						if len(p.Names) == 0 {
							sig.params = append(sig.params, param{typ: typ})
							continue
						}
						for _, n := range p.Names {
							sig.params = append(sig.params, param{name: n.Name, typ: typ})
						}
					}
				}
				if ft.Results != nil {
					for _, r := range ft.Results.List {
						typ := renderType(r.Type)
						n := len(r.Names)
						if n == 0 {
							n = 1
						}
						for i := 0; i < n; i++ {
							sig.results = append(sig.results, typ)
						}
					}
				}
				out[m.Names[0].Name] = sig
			}
		}
	}
	return out, nil
}

// generateMethods renders methods_gen.go for one dialect from the allowlist.
func generateMethods(d dialect, methods map[string]methodSig) ([]byte, error) {
	pkgName := filepath.Base(d.dir)
	var body strings.Builder
	needFmt, needRepo := false, false

	for _, name := range genMethods {
		sig, ok := methods[name]
		if !ok {
			return nil, fmt.Errorf("interface method %q not found", name)
		}
		if len(sig.params) == 0 {
			return nil, fmt.Errorf("method %q takes no context param", name)
		}
		var decls, names []string
		for i, p := range sig.params {
			pn := p.name
			if pn == "" {
				pn = fmt.Sprintf("a%d", i)
			}
			names = append(names, pn)
			decls = append(decls, pn+" "+p.typ)
		}
		ctxName := names[0]
		argSuffix := ""
		if len(names) > 1 {
			argSuffix = ", " + strings.Join(names[1:], ", ")
		}

		switch {
		case len(sig.results) == 1 && sig.results[0] == "error":
			fmt.Fprintf(&body, "\nfunc (a *%s) %s(%s) error {\n\treturn a.queries.%s(%s%s)\n}\n",
				d.adapterType, name, strings.Join(decls, ", "), name, ctxName, argSuffix)
		case len(sig.results) == 2 && sig.results[1] == "error" && strings.HasPrefix(sig.results[0], "*") && !strings.Contains(sig.results[0], "."):
			needFmt, needRepo = true, true
			// Interface is in package repository, so the result is the bare
			// "*Title"; qualify it as *repository.Title in the adapter package.
			dom := strings.TrimPrefix(sig.results[0], "*")
			fmt.Fprintf(&body, "\nfunc (a *%s) %s(%s) (*repository.%s, error) {\n", d.adapterType, name, strings.Join(decls, ", "), dom)
			fmt.Fprintf(&body, "\trow, err := a.queries.%s(%s%s)\n", name, ctxName, argSuffix)
			fmt.Fprintf(&body, "\tif err != nil {\n\t\treturn nil, fmt.Errorf(%q, err)\n\t}\n", d.name+" "+actionPhrase(name)+": %w")
			fmt.Fprintf(&body, "\treturn %s%sToDomain(row), nil\n}\n", d.name, dom)
		default:
			return nil, fmt.Errorf("method %q has an unsupported shape (results=%v); hand-write it", name, sig.results)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by repo-adapter-gen. DO NOT EDIT.\n\npackage %s\n\n", pkgName)
	b.WriteString("import (\n\t\"context\"\n")
	if needFmt {
		b.WriteString("\t\"fmt\"\n")
	}
	if needRepo {
		fmt.Fprintf(&b, "\t%q\n", "github.com/befabri/replayvod/server/internal/repository")
	}
	b.WriteString(")\n")
	b.WriteString(body.String())

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format methods source: %w\n%s", err, b.String())
	}
	return formatted, nil
}

// actionPhrase turns a method name into a lowercase space-separated phrase for
// error messages, e.g. UpsertTitle -> "upsert title".
func actionPhrase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte(' ')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
