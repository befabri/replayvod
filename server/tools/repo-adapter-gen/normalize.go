package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"sort"

	"golang.org/x/tools/go/ast/astutil"
)

// normalize applies normalizeFuncSrc under the renderer's config.
func (r renderer) normalize(src string) (string, error) {
	return normalizeFuncSrc(src, r.cfg.Equivalents)
}

// normalizeFuncSrc parses a single func declaration and renders it in a
// canonical form so that two functions compare equal exactly when they differ
// only in spelling: comments are dropped, positions are cleared so line
// breaks and grouping do not matter, every local identifier (receiver,
// parameters, variables) is renamed by order of first use, and helper calls
// that are definitionally the same as a literal are rewritten to the literal
// (see rewriteEquivalents and the config's equivalents). Package-level names,
// selectors, composite literal keys and literals are kept, so a swapped
// argument, a different error message or an extra statement still differs.
func normalizeFuncSrc(src string, eqs []equivalent) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "package x\n"+src, 0)
	if err != nil {
		return "", err
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		fd.Doc = nil
		splitGroupedFields(fd.Type.Params)
		rewriteEquivalents(fd, eqs)
		sortKeyedLiterals(fd)
		renameLocals(fd)
		clearPositions(fd)
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, token.NewFileSet(), fd); err != nil {
			return "", err
		}
		return buf.String(), nil
	}
	return "", fmt.Errorf("no func declaration in %q", src)
}

// splitGroupedFields expands "a, b T" into "a T, b T" so parameter grouping
// does not affect the rendered form.
func splitGroupedFields(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	var out []*ast.Field
	for _, f := range fl.List {
		if len(f.Names) <= 1 {
			out = append(out, f)
			continue
		}
		for _, n := range f.Names {
			out = append(out, &ast.Field{Names: []*ast.Ident{n}, Type: f.Type})
		}
	}
	fl.List = out
}

// rewriteEquivalents replaces calls whose result is, by the helper's
// definition, a fixed literal of their argument, as the config's equivalents
// declare: toNullString(&x) always builds a valid sql.NullString, so it is
// rewritten to that literal, which is the form the generator emits.
func rewriteEquivalents(fd *ast.FuncDecl, eqs []equivalent) {
	literals := make(map[string]string, len(eqs))
	for _, e := range eqs {
		literals[e.Helper] = e.Literal
	}
	astutil.Apply(fd, nil, func(c *astutil.Cursor) bool {
		call, ok := c.Node().(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		addr, ok := call.Args[0].(*ast.UnaryExpr)
		if !ok || addr.Op != token.AND {
			return true
		}
		literal, ok := literals[fn.Name]
		if !ok {
			return true
		}
		c.Replace(equivalentLiteral(literal, addr.X))
		return true
	})
}

// equivalentArg is the placeholder an equivalent's literal is parsed with.
const equivalentArg = "equivalentArg"

// equivalentLiteral instantiates literal with arg standing for %s. The
// template was parsed once when the config loaded.
func equivalentLiteral(literal string, arg ast.Expr) ast.Expr {
	expr, err := parser.ParseExpr(fmt.Sprintf(literal, equivalentArg))
	if err != nil {
		panic(err)
	}
	return astutil.Apply(expr, nil, func(c *astutil.Cursor) bool {
		if id, ok := c.Node().(*ast.Ident); ok && id.Name == equivalentArg {
			c.Replace(arg)
		}
		return true
	}).(ast.Expr)
}

// sortKeyedLiterals orders the elements of fully keyed composite literals by
// key. Field order carries no meaning in a keyed struct literal, and the
// values the generator emits are arguments and pure conversions of them, so
// evaluation order cannot matter either.
func sortKeyedLiterals(fd *ast.FuncDecl) {
	ast.Inspect(fd, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if _, ok := kv.Key.(*ast.Ident); !ok {
				return true
			}
		}
		sort.SliceStable(lit.Elts, func(i, j int) bool {
			return lit.Elts[i].(*ast.KeyValueExpr).Key.(*ast.Ident).Name < lit.Elts[j].(*ast.KeyValueExpr).Key.(*ast.Ident).Name
		})
		return true
	})
}

// renameLocals renames every identifier declared inside fd (receiver,
// parameters, results, := and var declarations, range variables, function
// literal parameters) to v0, v1, ... in order of first appearance. Selector
// fields and composite literal keys keep their names: they belong to types,
// not to the function's scope.
func renameLocals(fd *ast.FuncDecl) {
	declared := map[string]bool{}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				declared[n.Name] = true
			}
		}
	}
	addFields(fd.Recv)
	addFields(fd.Type.Params)
	addFields(fd.Type.Results)
	if fd.Body != nil {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				if s.Tok == token.DEFINE {
					for _, l := range s.Lhs {
						if id, ok := l.(*ast.Ident); ok {
							declared[id.Name] = true
						}
					}
				}
			case *ast.RangeStmt:
				if s.Tok == token.DEFINE {
					for _, e := range []ast.Expr{s.Key, s.Value} {
						if id, ok := e.(*ast.Ident); ok {
							declared[id.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				for _, id := range s.Names {
					declared[id.Name] = true
				}
			case *ast.FuncLit:
				addFields(s.Type.Params)
				addFields(s.Type.Results)
			}
			return true
		})
	}
	delete(declared, "_")
	names := map[string]string{}
	astutil.Apply(fd, func(c *astutil.Cursor) bool {
		id, ok := c.Node().(*ast.Ident)
		if !ok || !declared[id.Name] {
			return true
		}
		switch c.Parent().(type) {
		case *ast.KeyValueExpr:
			if c.Name() == "Key" {
				return true
			}
		case *ast.SelectorExpr:
			if c.Name() == "Sel" {
				return true
			}
		case *ast.FuncDecl:
			if c.Name() == "Name" {
				return true
			}
		}
		name, ok := names[id.Name]
		if !ok {
			name = fmt.Sprintf("v%d", len(names))
			names[id.Name] = name
		}
		id.Name = name
		return true
	}, nil)
}

var posType = reflect.TypeOf(token.NoPos)

// clearPositions zeroes every token.Pos in the tree so go/printer lays the
// code out from structure alone: multi-line literals collapse and blank
// lines disappear.
func clearPositions(n ast.Node) {
	ast.Inspect(n, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		v := reflect.ValueOf(n).Elem()
		for i := 0; i < v.NumField(); i++ {
			if f := v.Field(i); f.Type() == posType && f.CanSet() {
				f.SetInt(0)
			}
		}
		return true
	})
}
