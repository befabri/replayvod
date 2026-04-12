package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"reflect"
	"sort"
	"strings"
)

// explainDiff prints a structured summary of type-level changes between two
// generated Go files: added/removed types, added/removed fields, changed
// field types, changed validate tags. Scales the review experience when a
// snapshot regen produces a noisy byte-level diff.
//
// Does not attempt to diff function bodies, method sets, or import lists —
// the generator's output is structs-only by construction.
func explainDiff(oldPath, newPath string, out io.Writer) error {
	oldTypes, err := parseGeneratedTypes(oldPath)
	if err != nil {
		return fmt.Errorf("parse old: %w", err)
	}
	newTypes, err := parseGeneratedTypes(newPath)
	if err != nil {
		return fmt.Errorf("parse new: %w", err)
	}

	var added, removed, changed []string
	for name := range newTypes {
		if _, ok := oldTypes[name]; !ok {
			added = append(added, name)
		}
	}
	for name := range oldTypes {
		if _, ok := newTypes[name]; !ok {
			removed = append(removed, name)
		} else if !reflect.DeepEqual(oldTypes[name], newTypes[name]) {
			changed = append(changed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		fmt.Fprintln(out, "no type-level changes")
		return nil
	}

	if len(added) > 0 {
		fmt.Fprintf(out, "Added types (%d):\n", len(added))
		for _, name := range added {
			fmt.Fprintf(out, "  + %s (%d fields)\n", name, len(newTypes[name]))
		}
	}
	if len(removed) > 0 {
		fmt.Fprintf(out, "Removed types (%d):\n", len(removed))
		for _, name := range removed {
			fmt.Fprintf(out, "  - %s\n", name)
		}
	}
	if len(changed) > 0 {
		fmt.Fprintf(out, "Changed types (%d):\n", len(changed))
		for _, name := range changed {
			describeTypeDiff(out, name, oldTypes[name], newTypes[name])
		}
	}
	return nil
}

// fieldInfo captures just the bits we diff: Go type expression and validate
// tag body. JSON/url tags ride along with the generator so don't need
// dedicated diffing.
type fieldInfo struct {
	GoType      string
	ValidateTag string
}

// parseGeneratedTypes parses a Go file via go/ast and returns
// typeName → (fieldName → fieldInfo). Non-struct type declarations and
// anonymous fields are ignored.
func parseGeneratedTypes(path string) (map[string]map[string]fieldInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]fieldInfo{}
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
			fields := map[string]fieldInfo{}
			for _, field := range st.Fields.List {
				typeStr := exprString(field.Type)
				var validate string
				if field.Tag != nil {
					tag := strings.Trim(field.Tag.Value, "`")
					validate = reflect.StructTag(tag).Get("validate")
				}
				for _, n := range field.Names {
					fields[n.Name] = fieldInfo{GoType: typeStr, ValidateTag: validate}
				}
			}
			out[ts.Name.Name] = fields
		}
	}
	return out, nil
}

func describeTypeDiff(out io.Writer, name string, oldFields, newFields map[string]fieldInfo) {
	fmt.Fprintf(out, "  * %s:\n", name)

	var addedFields, removedFields []string
	for fname := range newFields {
		if _, ok := oldFields[fname]; !ok {
			addedFields = append(addedFields, fname)
		}
	}
	for fname := range oldFields {
		if _, ok := newFields[fname]; !ok {
			removedFields = append(removedFields, fname)
		}
	}
	sort.Strings(addedFields)
	sort.Strings(removedFields)

	for _, fname := range addedFields {
		fi := newFields[fname]
		fmt.Fprintf(out, "      + %s %s%s\n", fname, fi.GoType, tagSuffix(fi.ValidateTag))
	}
	for _, fname := range removedFields {
		fmt.Fprintf(out, "      - %s\n", fname)
	}

	var commonFields []string
	for fname := range newFields {
		if _, ok := oldFields[fname]; ok {
			commonFields = append(commonFields, fname)
		}
	}
	sort.Strings(commonFields)
	for _, fname := range commonFields {
		o, n := oldFields[fname], newFields[fname]
		if o.GoType != n.GoType {
			fmt.Fprintf(out, "      ~ %s: type %s -> %s\n", fname, o.GoType, n.GoType)
		}
		if o.ValidateTag != n.ValidateTag {
			fmt.Fprintf(out, "      ~ %s: validate %q -> %q\n", fname, o.ValidateTag, n.ValidateTag)
		}
	}
}

func tagSuffix(tag string) string {
	if tag == "" {
		return ""
	}
	return ` validate:"` + tag + `"`
}

// exprString renders an ast.Expr back to source form. Handles the field-type
// shapes the generator actually emits: Ident, SelectorExpr, ArrayType,
// StarExpr, MapType, InterfaceType.
func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.InterfaceType:
		return "any" // any / interface{} both emit this in the pool
	}
	return "<unknown>"
}
