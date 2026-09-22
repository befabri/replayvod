package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// valueObjects discovers the repository's immutable query parameters: structs
// with private fields, a value-receiver Validate() error, and read-only scalar
// accessors. Accessor names must match SQL parameter names, just as scalar
// arguments do. No query-specific field aliases or validation rules live here.
func valueObjects(dir string) (map[string][]param, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	private := map[string]bool{}
	methods := map[string]map[string]methodSig{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					t, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := t.Type.(*ast.StructType)
					if !ok {
						continue
					}
					allPrivate := len(st.Fields.List) > 0
					for _, f := range st.Fields.List {
						if len(f.Names) == 0 {
							allPrivate = false
						}
						for _, n := range f.Names {
							if n.IsExported() {
								allPrivate = false
							}
						}
					}
					private[t.Name.Name] = allPrivate
				}
			case *ast.FuncDecl:
				if d.Recv == nil || !d.Name.IsExported() {
					continue
				}
				receiver, ok := d.Recv.List[0].Type.(*ast.Ident)
				if !ok {
					continue
				}
				if methods[receiver.Name] == nil {
					methods[receiver.Name] = map[string]methodSig{}
				}
				methods[receiver.Name][d.Name.Name] = funcSig(d.Type)
			}
		}
	}
	out := map[string][]param{}
	for typ, members := range methods {
		validate, ok := members["Validate"]
		if !private[typ] || !ok || len(validate.params) != 0 || len(validate.results) != 1 || validate.results[0] != "error" {
			continue
		}
		var accessors []param
		for name, sig := range members {
			if name != "Validate" && len(sig.params) == 0 && len(sig.results) == 1 && scalarResults[sig.results[0]] {
				// Slices are mutable; pagination value objects expose scalar values.
				if !strings.HasPrefix(sig.results[0], "[]") {
					accessors = append(accessors, param{name: name, typ: sig.results[0]})
				}
			}
		}
		if len(accessors) > 0 {
			sort.Slice(accessors, func(i, j int) bool { return accessors[i].name < accessors[j].name })
			out[typ] = accessors
		}
	}
	return out, nil
}

type queryArg struct {
	name, typ, expr string
}

func (r renderer) queryArgs(sig methodSig, names []string) []queryArg {
	var args []queryArg
	for i, p := range sig.params[1:] {
		name := names[i+1]
		if accessors, ok := r.values[p.typ]; ok {
			for _, accessor := range accessors {
				args = append(args, queryArg{accessor.name, accessor.typ, name + "." + accessor.name + "()"})
			}
		} else {
			args = append(args, queryArg{name, p.typ, name})
		}
	}
	return args
}

// valueParams names the value-object parameters of sig, in declaration order.
func (r renderer) valueParams(sig methodSig) []string {
	var out []string
	for i, p := range sig.params {
		if _, ok := r.values[p.typ]; !ok {
			continue
		}
		name := p.name
		if name == "" {
			name = fmt.Sprintf("a%d", i)
		}
		out = append(out, name)
	}
	return out
}
