// repo-adapter-gen generates repository row mappers and adapter methods.
// It harvests handwritten methods only when their generated bodies match.
//
// Usage:
//
//	go run ./tools/repo-adapter-gen
//	go run ./tools/repo-adapter-gen -check
//
// handwritten_baseline.txt records how many Repository methods each adapter
// still implements by hand; a run lowers the numbers and -check fails when
// they rise.
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
	"regexp"
	"sort"
	"strconv"
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
	// slice also emits pg<plural>ToDomain([]row) []domain, which calls the
	// single-row mapper. plural defaults to name+"s"; it only makes sense when
	// the row type is the plain singular row, which holds for every allowlisted
	// type because the list queries select whole table rows.
	slice  bool
	plural string
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

func (s genSpec) pluralName() string {
	if s.plural != "" {
		return s.plural
	}
	return s.name + "s"
}

// genTypes is the allowlist of types whose mappers are generated. A type belongs
// here only if every one of its domain fields maps to a row field with a known
// conversion (see convRules). Complex/non-1:1 types stay hand-written.
var genTypes = []genSpec{
	{name: "Title"},
	{name: "Tag", slice: true},
	{name: "Category", slice: true, plural: "Categories"},
	{name: "Channel", slice: true},
	{name: "ChannelUserState"},
	{name: "EventLog", slice: true},
	{name: "Job"},
	{name: "MediaPublication", slice: true},
	{name: "RecordingIntent", slice: true},
	{name: "RecordingWebhookDelivery"},
	{name: "Stream", slice: true},
	{name: "Subscription", slice: true},
	{name: "Task", slice: true},
	{name: "User", slice: true},
	{name: "VideoPart", slice: true},
	{name: "VideoPlaybackAsset"},
	{name: "VideoUserState"},
	{name: "WebhookEvent", slice: true},
	{name: "ServerSettings", row: "ServerSetting"},
	{name: "Settings", row: "Setting"},
	{name: "Snapshot", domain: "EventSubSnapshot", row: "EventsubSnapshot", slice: true},
}

// mapperSpec returns the genTypes entry whose domain type is domain, if any.
func mapperSpec(domain string) (genSpec, bool) {
	for _, s := range genTypes {
		if s.domainType() == domain {
			return s, true
		}
	}
	return genSpec{}, false
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
	{"sqlitetype.Time", "time.Time"}:          "%s.Time",
	{"*sqlitetype.Time", "*time.Time"}:        "timePtrFromSQLite(%s)",
	{"*sqlitetype.PreciseTime", "*time.Time"}: "timePtrFromSQLitePrecise(%s)",
	{"int64", "bool"}:                         "%s != 0",
	{"sql.NullInt64", "*int64"}:               "fromNullInt64(%s)",
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

// argRules is the argument-side mirror of convRules: it maps
// {repositoryParamType, sqlcParamType} to the expression that passes a
// repository argument to a sqlc query, with %s standing for the argument.
// Identical types need no entry. Every SQLite helper named here is defined in
// the sqliteadapter package.
var argRules = map[[2]string]string{
	// numeric width (repository int -> PG int32 / SQLite int64)
	{"int", "int32"}:   "int32(%s)",
	{"int", "int64"}:   "int64(%s)",
	{"int64", "int32"}: "int32(%s)",
	{"int32", "int64"}: "int64(%s)",
	// PG nullable columns take pointers to the caller's value
	{"string", "*string"}:       "&%s",
	{"time.Time", "*time.Time"}: "&%s",
	// SQLite glue
	{"bool", "int64"}:                         "boolToInt64(%s)",
	{"json.RawMessage", "string"}:             "string(%s)",
	{"time.Time", "sqlitetype.Time"}:          "sqliteTime(%s)",
	{"time.Time", "*sqlitetype.Time"}:         "sqliteTimePtr(&%s)",
	{"*time.Time", "*sqlitetype.Time"}:        "sqliteTimePtr(%s)",
	{"time.Time", "*sqlitetype.PreciseTime"}:  "sqlitePreciseTimePtr(&%s)",
	{"*time.Time", "*sqlitetype.PreciseTime"}: "sqlitePreciseTimePtr(%s)",
	{"string", "sql.NullString"}:              "sql.NullString{String: %s, Valid: true}",
	{"*string", "sql.NullString"}:             "toNullString(%s)",
	{"int64", "sql.NullInt64"}:                "sql.NullInt64{Int64: %s, Valid: true}",
	{"*int64", "sql.NullInt64"}:               "toNullInt64(%s)",
	{"float64", "sql.NullFloat64"}:            "sql.NullFloat64{Float64: %s, Valid: true}",
	{"*float64", "sql.NullFloat64"}:           "nullFloat64(%s)",
}

// convertArg renders a repository argument of type from as the sqlc parameter
// type to, or reports that no rule exists.
func convertArg(from, to, expr string) (string, bool) {
	if from == to {
		return expr, true
	}
	tmpl, ok := argRules[[2]string{from, to}]
	if !ok {
		return "", false
	}
	return fmt.Sprintf(tmpl, expr), true
}

// queryAliases maps repository methods to the sqlc query they call when the
// two names differ. The list is deliberately short: a broad rename manifest
// would let a method be generated against a query whose parameters happen to
// share types with its own, which the name-equality guard otherwise prevents.
var queryAliases = map[string]string{
	"CreateEventSubSnapshot":     "CreateSnapshot",
	"GetLatestEventSubSnapshot":  "GetLatestSnapshot",
	"ListEventSubSnapshots":      "ListSnapshots",
	"DeleteOldEventSubSnapshots": "DeleteOldSnapshots",
}

type dialect struct {
	name        string // "pg" / "sqlite"
	dir         string // adapter package dir
	genPkg      string // "pggen" / "sqlitegen"
	genAlias    string // import path of the gen package
	adapterType string // "PGAdapter" / "SQLiteAdapter"
	// rowLocks names the queries whose Postgres text carries a row-locking
	// clause. Their generated methods refuse to run outside WithTx on every
	// dialect, because a lock taken on an autocommit connection is released
	// before it is used.
	rowLocks map[string]bool
}

// Method generation is auto-discovered: there is no allowlist. Every
// repository.Repository method is tried against the sqlc query of the same
// name (or its queryAliases entry) and rendered in every supported shape: exec,
// rows-affected exec, one row, slice, direct scalar and rows-affected bool,
// each with the error-handling styles the adapters use (see shapes.go).
// A method is emitted into methods_gen.go only when one of:
//
//   - it is already present in methods_gen.go (harvested on a prior run), in
//     which case the shape it was harvested in is re-emitted, or
//   - it is still hand-written AND one rendered shape is structurally
//     identical to the hand-written body (see normalizeFuncSrc) — in which
//     case the hand-written copy is also deleted (the "harvest").
//
// Structural identity makes harvesting behavior-preserving by construction:
// any method carrying extra logic, a different error message, a swapped
// argument or an unknown conversion simply differs and is left hand-written. A
// brand-new method that fits a shape but has no implementation yet is NOT
// guessed; write it by hand first and the next run harvests it if it matches.
//
// denyMethods force-excludes names that would otherwise be harvested but must
// stay hand-written (e.g. a trivial-looking method expected to grow logic).
var denyMethods = map[string]bool{
	// Repository.WithTx owns a transaction and passes a scoped repository
	// to a callback; sqlc's unrelated WithTx only binds a query object.
	"WithTx": true,
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

	rowLocks, err := rowLockQueries(filepath.Join(*root, "internal/repository/pgadapter/pggen"))
	if err != nil {
		fail(err)
	}
	dialects := []dialect{
		{name: "pg", dir: "internal/repository/pgadapter", genPkg: "pggen", adapterType: "PGAdapter",
			genAlias: "github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen", rowLocks: rowLocks},
		{name: "sqlite", dir: "internal/repository/sqliteadapter", genPkg: "sqlitegen", adapterType: "SQLiteAdapter",
			genAlias: "github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen", rowLocks: rowLocks},
	}

	handCounts := map[string]int{}
	for _, d := range dialects {
		// sqlc places model and query parameter structs in separate files.
		genDir := filepath.Join(*root, d.dir, d.genPkg)
		gen, err := structFieldsDir(genDir)
		if err != nil {
			fail(err)
		}
		queries, err := queryMethods(genDir)
		if err != nil {
			fail(err)
		}
		mapperSrc, err := generate(d, domain, gen)
		if err != nil {
			fail(fmt.Errorf("%s mappers: %w", d.name, err))
		}
		methodSrc, harvest, handCount, err := generateMethods(d, methods, gen, queries, *root)
		if err != nil {
			fail(fmt.Errorf("%s methods: %w", d.name, err))
		}
		handCounts[filepath.Base(d.dir)] = handCount
		outputs := []struct {
			path string
			src  []byte
			note string
		}{
			{filepath.Join(*root, d.dir, "mappers_gen.go"), mapperSrc, fmt.Sprintf("%d types", len(genTypes))},
			{filepath.Join(*root, d.dir, "methods_gen.go"), methodSrc, fmt.Sprintf("%d methods", strings.Count(string(methodSrc), "\nfunc (a *"))},
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
		// Check mode must leave handwritten files untouched, including pending harvests.
		if !*check {
			if err := applyHarvest(harvest); err != nil {
				fail(fmt.Errorf("%s harvest: %w", d.name, err))
			}
		}
	}
	if err := ratchet(filepath.Join(*root, "tools/repo-adapter-gen", baselineFile), handCounts, *check); err != nil {
		fail(err)
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
		// sqlc and domain structs differ in initialism casing, such as BoxArtUrl/BoxArtURL.
		rowByNorm := make(map[string]string, len(rowFields))
		for rf := range rowFields {
			rowByNorm[strings.ToLower(rf)] = rf
		}
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
			body.WriteString(sliceMapperSrc(d, spec))
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

// sliceMapperSrc renders the generated slice mapper for spec. Its body is the
// make-and-loop form, so an adapter method that spells the loop inline is
// structurally the same as one that calls the mapper (see inlineLoopSrc).
func sliceMapperSrc(d dialect, spec genSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nfunc %s%sToDomain(rows []%s.%s) []repository.%s {\n",
		d.name, spec.pluralName(), d.genPkg, spec.rowType(), spec.domainType())
	fmt.Fprintf(&b, "\tout := make([]repository.%s, len(rows))\n", spec.domainType())
	fmt.Fprintf(&b, "\tfor i, r := range rows {\n\t\tout[i] = *%s%sToDomain(r)\n\t}\n\treturn out\n}\n",
		d.name, spec.name)
	return b.String()
}

// structFieldsDir parses every .go file in dir and merges their struct
// definitions. Used for the sqlc gen package, where row structs live in
// models.go and query param structs live in the per-query .sql.go files.
func structFieldsDir(dir string) (map[string]map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		m, err := structFields(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			out[k] = v
		}
	}
	return out, nil
}

// lockClause matches Postgres row-locking clauses, including NOWAIT and OF
// variants. Claiming queries add SKIP LOCKED after the match and are excluded
// by rowLockQueries because they take their own transaction.
var lockClause = regexp.MustCompile(`\bFOR (NO KEY )?UPDATE\b|\bFOR (KEY )?SHARE\b`)

// rowLockQueries returns the sqlc query names in dir whose SQL locks rows for
// the caller's transaction. The Postgres text is the source of truth for
// locking intent; SQLite emulates the same queries with a no-op
// UPDATE ... RETURNING, so the set applies to both dialects.
func rowLockQueries(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				sql, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", e.Name(), err)
				}
				header, _, _ := strings.Cut(sql, "\n")
				name, ok := strings.CutPrefix(header, "-- name: ")
				if !ok {
					continue
				}
				name, _, _ = strings.Cut(name, " ")
				if locs := lockClause.FindAllStringIndex(sql, -1); len(locs) > 0 {
					tail := sql[locs[len(locs)-1][1]:]
					if !strings.Contains(tail, "SKIP LOCKED") {
						out[name] = true
					}
				}
			}
		}
	}
	return out, nil
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
// identifiers, pointers, selectors (pkg.Type), and slices.
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

// funcSig renders a function type's parameters and results, one entry per
// name so grouped declarations ("limit, offset int") expand.
func funcSig(ft *ast.FuncType) methodSig {
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
	return sig
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
				out[m.Names[0].Name] = funcSig(ft)
			}
		}
	}
	return out, nil
}

// recvTypeName returns the receiver's base type name (e.g. "PGAdapter" for
// "func (a *PGAdapter) ..."), or "" if recv is not a single named type.
func recvTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) != 1 {
		return ""
	}
	switch t := recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// camelWords splits a Go identifier into its words. An uppercase run is one
// initialism ("HMAC", "ID"), including a plural spelled with a trailing "s"
// ("IDs"); otherwise the run's last letter begins the next word ("HMACSecret"
// is "HMAC", "Secret").
func camelWords(name string) []string {
	rs := []rune(name)
	var words []string
	start := 0
	for i := 1; i < len(rs); i++ {
		prevUpper := unicode.IsUpper(rs[i-1])
		curUpper := unicode.IsUpper(rs[i])
		switch {
		case curUpper && !prevUpper:
			words = append(words, string(rs[start:i]))
			start = i
		case !curUpper && prevUpper && i-start >= 2:
			pluralInitialism := rs[i] == 's' && (i+1 == len(rs) || unicode.IsUpper(rs[i+1]))
			if !pluralInitialism {
				words = append(words, string(rs[start:i-1]))
				start = i - 1
			}
		}
	}
	return append(words, string(rs[start:]))
}

// isInitialism reports whether a word from camelWords is an initialism, with
// or without its plural "s".
func isInitialism(word string) bool {
	w := strings.TrimSuffix(word, "s")
	return len(w) >= 2 && strings.ToUpper(w) == w
}

// actionPhrases turns a method name into the space-separated phrases the
// adapters use in error messages, e.g. UpsertTitle -> "upsert title". A name
// with initialisms yields two spellings: all lowercase ("set storage id") and
// with the initialisms kept ("set storage ID").
func actionPhrases(name string) []string {
	words := camelWords(name)
	lower := make([]string, len(words))
	kept := make([]string, len(words))
	for i, w := range words {
		lower[i] = strings.ToLower(w)
		kept[i] = lower[i]
		if isInitialism(w) {
			kept[i] = w
		}
	}
	out := []string{strings.Join(lower, " ")}
	if k := strings.Join(kept, " "); k != out[0] {
		out = append(out, k)
	}
	return out
}
