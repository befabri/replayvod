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

const allowlistPath = "testdata/uncovered.txt"

// forbiddenImportPrefix rejects the adapter packages: with a concrete adapter
// in scope, a selector call named like a Repository method might not be one.
const forbiddenImportPrefix = "github.com/befabri/replayvod/server/internal/repository/"

// TestContractCoverageRatchet fails when a Repository method has no contract
// test and is not listed in testdata/uncovered.txt, and when that list holds an
// entry that is covered or is no longer a method. A method is covered when a
// non-test file in this package calls a selector with its name on something
// other than an imported package. Name matching is sound only while this
// package references no concrete adapter and neither Harness nor *testing.T
// shares a method name with Repository; both are checked.
func TestContractCoverageRatchet(t *testing.T) {
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
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
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
	allowed := readAllowlist(t)
	clusters := interfaceClusters(t)

	var unlisted, covered, stale []string
	for _, name := range uncovered {
		if !slices.Contains(allowed, name) {
			unlisted = append(unlisted, name)
		}
	}
	for _, name := range allowed {
		switch {
		case !slices.Contains(methods, name):
			stale = append(stale, name)
		case called[name]:
			covered = append(covered, name)
		}
	}
	if len(unlisted) > 0 {
		t.Errorf("Repository methods without a contract test (%d); add one for each rather than growing %s:\n%s",
			len(unlisted), allowlistPath, clusters.report(unlisted))
	}
	if len(covered) > 0 {
		t.Errorf("entries in %s that now have a contract test (%d); remove them, the list only shrinks:\n%s",
			allowlistPath, len(covered), clusters.report(covered))
	}
	if len(stale) > 0 {
		t.Errorf("entries in %s that are not Repository methods (%d); remove them:\n\t%s",
			allowlistPath, len(stale), strings.Join(stale, "\n\t"))
	}
	t.Logf("%d of %d Repository methods have no contract test", len(uncovered), len(methods))
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

// readAllowlist reads allowlistPath, one name per line, # starting a comment.
func readAllowlist(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(allowlistPath)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		if at := strings.IndexByte(line, '#'); at >= 0 {
			line = line[:at]
		}
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		switch {
		case seen[name]:
			t.Errorf("%s:%d: duplicate entry %s", allowlistPath, i+1, name)
		case len(names) > 0 && names[len(names)-1] > name:
			t.Errorf("%s:%d: %s is out of order; keep the file sorted", allowlistPath, i+1, name)
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
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
