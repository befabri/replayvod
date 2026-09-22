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
// arguments and the exported fields of domain struct parameters do. No
// query-specific field aliases or validation rules live here.
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
	// optional marks a field expanded from a domain struct. The query decides
	// which fields it needs, so an unused one is not an error, unlike a scalar
	// or accessor that would otherwise be silently dropped.
	optional bool
}

// queryArgs lists the values a method can pass to its query: scalars as
// themselves, value objects through their accessors, and domain structs
// through their exported fields.
func (r renderer) queryArgs(sig methodSig, names []string) []queryArg {
	var args []queryArg
	for i, p := range sig.params[1:] {
		name := names[i+1]
		if accessors, ok := r.values[p.typ]; ok {
			for _, accessor := range accessors {
				args = append(args, queryArg{name: accessor.name, typ: accessor.typ, expr: name + "." + accessor.name + "()"})
			}
			continue
		}
		if fields, ok := r.structParam(p.typ); ok {
			for _, f := range fields {
				args = append(args, queryArg{name: f.name, typ: f.typ, expr: name + "." + f.name, optional: true})
			}
			continue
		}
		args = append(args, queryArg{name: name, typ: p.typ, expr: name})
	}
	return args
}

// structParam returns the exported fields, by name, of a domain struct
// parameter declared as the struct or a pointer to it. Slices and value
// objects are not struct parameters.
func (r renderer) structParam(typ string) ([]param, bool) {
	base := strings.TrimPrefix(typ, "*")
	if strings.ContainsAny(base, "*[].") {
		return nil, false
	}
	if _, value := r.values[base]; value {
		return nil, false
	}
	fields, ok := r.domain[base]
	if !ok || len(fields) == 0 {
		return nil, false
	}
	out := make([]param, 0, len(fields))
	for name, typ := range fields {
		out = append(out, param{name: name, typ: typ})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, true
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
