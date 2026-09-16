package contracttest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// repositorySource is parsed only for the blank-line layout of the interface;
// the method set itself comes from reflection.
const repositorySource = "../repository.go"

// forbiddenImportPrefix rejects the adapter packages: with a concrete adapter
// in scope, a selector call named like a Repository method might not be one.
const forbiddenImportPrefix = "github.com/befabri/replayvod/server/internal/repository/"

// TestContractCoverage fails when a Repository method has no contract test or
// a contract test is not registered to run. A method is covered when a
// non-test file in this package calls a selector with its name on something
// other than an imported package; a test is a top-level func named test* that
// takes a *testing.T and a Harness, and it runs only when Run registers it.
// Name matching is sound only while this package references no concrete
// adapter and neither Harness nor *testing.T shares a method name with
// Repository; both are checked.
func TestContractCoverage(t *testing.T) {
	methods := methodNames(reflect.TypeFor[repository.Repository]())
	for _, other := range []reflect.Type{reflect.TypeFor[Harness](), reflect.TypeFor[*testing.T]()} {
		for _, name := range methodNames(other) {
			if slices.Contains(methods, name) {
				t.Errorf("%s.%s shares its name with a Repository method, so call-name matching cannot tell them apart", other, name)
			}
		}
	}

	fset := token.NewFileSet()
	called := map[string]bool{}
	var tests, registered []string
	for _, file := range parsePackageFiles(t, fset) {
		packages := map[string]bool{}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(path, forbiddenImportPrefix) {
				t.Errorf("%s imports %s; contracttest must stay adapter-agnostic", fset.Position(imp.Pos()).Filename, path)
			}
			packages[importName(imp, path)] = true
		}
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && isContractTest(fd) {
				tests = append(tests, fd.Name.Name)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "run" && len(call.Args) == 2 {
				if test, ok := call.Args[1].(*ast.Ident); ok {
					registered = append(registered, test.Name)
				}
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// A package-level function shares no receiver with Repository, so
			// its name cannot stand in for a method.
			if ident, ok := sel.X.(*ast.Ident); ok && packages[ident.Name] {
				return true
			}
			called[sel.Sel.Name] = true
			return true
		})
	}

	var uncovered []string
	for _, name := range methods {
		if !called[name] {
			uncovered = append(uncovered, name)
		}
	}
	if len(uncovered) > 0 {
		t.Errorf("Repository methods without a contract test (%d):\n%s", len(uncovered), interfaceClusters(t).report(uncovered))
	}
	for _, test := range tests {
		if !slices.Contains(registered, test) {
			t.Errorf("%s is never registered with run in Run, so it covers nothing", test)
		}
	}
	slices.Sort(registered)
	for i := 1; i < len(registered); i++ {
		if registered[i] == registered[i-1] {
			t.Errorf("%s is registered more than once", registered[i])
		}
	}
	t.Logf("%d Repository methods, %d contract tests", len(methods), len(tests))
}

// isContractTest reports whether fd is a contract test: a top-level func
// named test* taking a *testing.T and a Harness.
func isContractTest(fd *ast.FuncDecl) bool {
	if fd.Recv != nil || !strings.HasPrefix(fd.Name.Name, "test") || fd.Type.Params.NumFields() != 2 {
		return false
	}
	second := fd.Type.Params.List[len(fd.Type.Params.List)-1].Type
	ident, ok := second.(*ast.Ident)
	return ok && ident.Name == "Harness"
}

// importName returns the identifier an import binds in the file: its alias
// when given, otherwise the last path element. A package whose name differs
// from that element is not recognised, which only widens the match back to
// name-only counting for its calls.
func importName(imp *ast.ImportSpec, path string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	return path[strings.LastIndex(path, "/")+1:]
}

func methodNames(typ reflect.Type) []string {
	names := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		names = append(names, typ.Method(i).Name)
	}
	return names
}

func parsePackageFiles(t *testing.T, fset *token.FileSet) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatal("no contract test sources found")
	}
	return files
}

// methodCluster is one blank-line-separated run of interface methods, the only
// domain grouping repository.go has.
type methodCluster struct {
	line    int
	methods []string
}

type methodClusters []methodCluster

// interfaceClusters splits the interface at blank lines, counting a method's
// doc comment as part of its own cluster rather than as a gap.
func interfaceClusters(t *testing.T) methodClusters {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, repositorySource, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var iface *ast.InterfaceType
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok && spec.Name.Name == "Repository" {
			iface, _ = spec.Type.(*ast.InterfaceType)
			return false
		}
		return iface == nil
	})
	if iface == nil {
		t.Fatalf("%s: no Repository interface", repositorySource)
	}
	var clusters methodClusters
	prevEnd := 0
	for _, field := range iface.Methods.List {
		start := field.Pos()
		if field.Doc != nil {
			start = field.Doc.Pos()
		}
		line := fset.Position(start).Line
		if len(clusters) == 0 || line > prevEnd+1 {
			clusters = append(clusters, methodCluster{line: fset.Position(field.Pos()).Line})
		}
		last := &clusters[len(clusters)-1]
		for _, name := range field.Names {
			last.methods = append(last.methods, name.Name)
		}
		prevEnd = fset.Position(field.End()).Line
	}
	return clusters
}

func (clusters methodClusters) report(names []string) string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, cluster := range clusters {
		var hits []string
		for _, name := range cluster.methods {
			if slices.Contains(names, name) {
				hits = append(hits, name)
				seen[name] = true
			}
		}
		if len(hits) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s:%d %s cluster, %d of %d:\n\t%s\n",
			filepath.Base(repositorySource), cluster.line, cluster.methods[0], len(hits), len(cluster.methods), strings.Join(hits, "\n\t"))
	}
	var rest []string
	for _, name := range names {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	if len(rest) > 0 {
		fmt.Fprintf(&b, "not in the Repository interface source:\n\t%s\n", strings.Join(rest, "\n\t"))
	}
	return b.String()
}
