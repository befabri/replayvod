// repo-adapter-gen generates repository row mappers and adapter methods.
// It harvests handwritten methods only when their generated bodies match.
//
// Usage:
//
//	go run ./tools/repo-adapter-gen
//	go run ./tools/repo-adapter-gen -check
//
// repo-adapter-gen.yaml in the project root describes the repository layout,
// the adapter conventions and the type conversions (see config.go); the
// engine carries no project names. The baseline file it names records how
// many interface methods each adapter still implements by hand; a run lowers
// the numbers and -check fails when they rise.
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
// (<dialect><name><mapper suffix>). domain/row override the domain struct and
// sqlc row struct names when they differ from name (e.g. domain
// EventSubSnapshot from sqlc row EventsubSnapshot, exposed as Snapshot). Empty
// domain/row default to name.
type genSpec struct {
	name   string
	domain string
	row    string
	// slice also emits <dialect><plural>ToDomain([]row) []domain, which calls
	// the single-row mapper. plural defaults to name+"s". A row may be a
	// whole-table model or a per-query projection; each domain/row pair has
	// its own mapper.
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

// dialect is one adapter package and the sqlc output it wraps, resolved from
// the config's dialects.
type dialect struct {
	name        string // prefixes mapper names and error messages
	dir         string // adapter package dir
	genPkg      string // sqlc package name
	genAlias    string // import path of the gen package
	adapterType string // receiver type of the adapter methods
	// rowLocks names the sqlc queries that lock rows for the caller's
	// transaction; their generated bodies guard against running outside one.
	rowLocks map[string]bool
}

// Method generation is auto-discovered: there is no allowlist. Every interface
// method is tried against the sqlc query of the same name (or its alias in
// the config) and rendered in every supported shape: exec, rows-affected
// exec, discarded-row exec, one row, slice (optionally short-circuiting an
// empty slice argument), direct scalar and rows-affected bool, each with the
// error-handling styles the adapters use (see shapes.go). Validated value
// objects expand through their scalar accessors and retain a Validate call
// before querying (see values.go); ordinary structs stay manual.
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
// The config's deny list force-excludes names that would otherwise be
// harvested but must stay hand-written (e.g. a trivial-looking method
// expected to grow logic).
func main() {
	root := flag.String("root", ".", "project root; config paths are relative to it")
	cfgPath := flag.String("config", "repo-adapter-gen.yaml", "config file, relative to root")
	check := flag.Bool("check", false, "verify generated files are up to date instead of writing")
	flag.Parse()

	cfg, err := loadConfig(filepath.Join(*root, *cfgPath))
	if err != nil {
		fail(err)
	}
	domain, err := structFields(filepath.Join(*root, cfg.Domain.Models))
	if err != nil {
		fail(err)
	}
	methods, err := interfaceMethods(filepath.Join(*root, cfg.Domain.InterfaceFile), cfg.Domain.Interface)
	if err != nil {
		fail(err)
	}
	values, err := valueObjects(filepath.Join(*root, cfg.Domain.ValueObjects))
	if err != nil {
		fail(err)
	}
	dialects, err := cfg.dialects(*root)
	if err != nil {
		fail(err)
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
		r := renderer{cfg: cfg, d: d, domain: domain, gen: gen, queries: queries, values: values}
		mapperSrc, err := r.generateMappers()
		if err != nil {
			fail(fmt.Errorf("%s mappers: %w", d.name, err))
		}
		methodSrc, harvest, handCount, err := r.generateMethods(methods, *root)
		if err != nil {
			fail(fmt.Errorf("%s methods: %w", d.name, err))
		}
		handCounts[filepath.Base(d.dir)] = handCount
		outputs := []struct {
			path string
			src  []byte
			note string
		}{
			{filepath.Join(*root, d.dir, "mappers_gen.go"), mapperSrc, fmt.Sprintf("%d types", len(cfg.types))},
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
	if err := ratchet(filepath.Join(*root, cfg.Baseline), handCounts, *check); err != nil {
		fail(err)
	}
}

// generateMappers renders the mappers_gen.go body for the renderer's dialect.
func (r renderer) generateMappers() ([]byte, error) {
	pkgName := filepath.Base(r.d.dir)
	var body strings.Builder

	for _, spec := range r.cfg.types {
		literal, err := r.mapperLiteral(spec, "src")
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&body, "\nfunc %s(src %s.%s) *%s {\n\treturn &%s\n}\n",
			r.mapperName(spec.name), r.d.genPkg, spec.rowType(), r.domainType(spec.domainType()), literal)

		if spec.slice {
			body.WriteString(r.sliceMapperSrc(spec))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by repo-adapter-gen. DO NOT EDIT.\n\npackage %s\n\n", pkgName)
	b.WriteString("import (\n")
	fmt.Fprintf(&b, "\t%q\n", r.cfg.Domain.Import)
	if strings.Contains(body.String(), "json.") {
		fmt.Fprintf(&b, "\t%q\n", "encoding/json")
	}
	fmt.Fprintf(&b, "\t%q\n", r.d.genAlias)
	b.WriteString(")\n")
	b.WriteString(body.String())

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w\n%s", err, b.String())
	}
	return formatted, nil
}

// mapperLiteral is shared by mapper generation and matching inline row
// literals during harvest. Fields match by name, allowing only initialism
// casing differences; SQL projections must alias other differences explicitly.
func (r renderer) mapperLiteral(spec genSpec, src string) (string, error) {
	domFields, ok := r.domain[spec.domainType()]
	if !ok {
		return "", fmt.Errorf("domain type %q not found", spec.domainType())
	}
	rowFields, ok := r.gen[spec.rowType()]
	if !ok {
		return "", fmt.Errorf("row type %q not found", spec.rowType())
	}
	rowByNorm := make(map[string]string, len(rowFields))
	for rf := range rowFields {
		rowByNorm[strings.ToLower(rf)] = rf
	}
	names := make([]string, 0, len(domFields))
	for f := range domFields {
		names = append(names, f)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "%s{\n", r.domainType(spec.domainType()))
	for _, f := range names {
		rowName, ok := rowByNorm[strings.ToLower(f)]
		if !ok {
			return "", fmt.Errorf("type %s: domain field %q has no row field; alias the SQL column or hand-write the mapper", spec.name, f)
		}
		rowType, domType := rowFields[rowName], domFields[f]
		tmpl, ok := r.cfg.conversion(rowType, domType)
		if !ok {
			return "", fmt.Errorf("type %s field %q: no conversion rule for row %q -> domain %q", spec.name, f, rowType, domType)
		}
		fmt.Fprintf(&b, "%s: %s,\n", f, fmt.Sprintf(tmpl, src+"."+rowName))
	}
	b.WriteString("}")
	return b.String(), nil
}

// sliceMapperSrc renders the generated slice mapper for spec. Its body is the
// make-and-loop form, so an adapter method that spells the loop inline is
// structurally the same as one that calls the mapper (see sliceCandidates).
func (r renderer) sliceMapperSrc(spec genSpec) string {
	elem := r.domainType(spec.domainType())
	var b strings.Builder
	fmt.Fprintf(&b, "\nfunc %s(rows []%s.%s) []%s {\n", r.mapperName(spec.pluralName()), r.d.genPkg, spec.rowType(), elem)
	fmt.Fprintf(&b, "\tout := make([]%s, len(rows))\n", elem)
	fmt.Fprintf(&b, "\tfor i, r := range rows {\n\t\tout[i] = *%s(r)\n\t}\n\treturn out\n}\n", r.mapperName(spec.name))
	return b.String()
}

// structFieldsDir parses every .go file in dir and merges their struct
// definitions. Used for the sqlc gen package, where row structs live in
// models.go and query row/parameter structs live in the per-query .sql.go files.
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
