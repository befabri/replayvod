package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// querySig is the signature of one sqlc Queries method without its context
// parameter. Types are rendered as they appear inside the gen package, so row
// and parameter structs are unqualified.
type querySig struct {
	params  []param
	results []string
}

// queryMethods parses every sqlc query file in dir and returns the Queries
// method signatures by name.
func queryMethods(dir string) (map[string]querySig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]querySig{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || recvTypeName(fd.Recv) != "Queries" {
				continue
			}
			sig := funcSig(fd.Type)
			if len(sig.params) == 0 || sig.params[0].typ != "context.Context" {
				continue
			}
			out[fd.Name.Name] = querySig{params: sig.params[1:], results: sig.results}
		}
	}
	return out, nil
}
