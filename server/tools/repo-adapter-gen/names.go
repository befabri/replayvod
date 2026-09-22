package main

import (
	"fmt"
	"sort"
	"strings"
)

// isScalarType reports whether a repository parameter type is a value the
// generator could pass to a query on its own: a predeclared type, a type from
// another package such as time.Time, or a pointer or slice of one. Domain
// structs are not scalars; validated value objects are expanded separately.
func isScalarType(typ string) bool {
	base := strings.TrimLeft(typ, "*[]")
	return base != "" && !strings.HasPrefix(base, "<") && !isDomainType(base)
}

// misnamedParams lists the scalar parameters of name that its query's Params
// struct spells differently, which is what leaves a method hand-written after
// every other shape matches. Only a Params struct is compared. Value-object
// accessors and domain struct fields are expanded, but a struct field the
// query does not use is not misnamed: the query decides which fields it
// needs. A scalar counts when some field has a type it converts to: a value
// the adapter derives by hand, such as a time passed on as milliseconds,
// never reaches the query under its own name, so it has nothing to be named
// after.
func (r renderer) misnamedParams(name string, sig methodSig) []string {
	if len(sig.params) == 0 || sig.params[0].typ != "context.Context" {
		return nil
	}
	qname := r.cfg.alias(name)
	q, ok := r.queries[qname]
	if !ok || len(q.params) != 1 || q.params[0].typ != qname+"Params" {
		return nil
	}
	fields, ok := r.gen[qname+"Params"]
	if !ok {
		return nil
	}
	fieldByNorm := make(map[string]string, len(fields))
	fieldNames := make([]string, 0, len(fields))
	for f := range fields {
		fieldByNorm[strings.ToLower(f)] = f
		fieldNames = append(fieldNames, f)
	}
	sort.Strings(fieldNames)
	var out []string
	names := make([]string, len(sig.params))
	for i, p := range sig.params {
		names[i] = p.name
	}
	for _, p := range r.queryArgs(sig, names) {
		if p.optional || !isScalarType(p.typ) {
			continue
		}
		if _, ok := fieldByNorm[strings.ToLower(p.name)]; ok {
			continue
		}
		convertible := false
		for _, ft := range fields {
			if _, ok := r.cfg.convertArg(p.typ, ft, p.name); ok {
				convertible = true
				break
			}
		}
		if !convertible {
			continue
		}
		out = append(out, fmt.Sprintf("%s: parameter %s has no field in %sParams (%s)", name, p.name, qname, strings.Join(fieldNames, ", ")))
	}
	return out
}
