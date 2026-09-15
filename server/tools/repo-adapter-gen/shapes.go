package main

import (
	"fmt"
	"strings"
)

// candidate is one generated form of an adapter method. emit is the source
// written to methods_gen.go; match lists further hand-written spellings the
// generator accepts as the same method, such as the inline loop a generated
// slice mapper replaces or the if-return spelling of a plain exec.
type candidate struct {
	emit  string
	match []string
}

// scalarResults are the repository result types the scalar shape can return:
// a query result passed through unchanged or through resultRules.
var scalarResults = map[string]bool{
	"int64": true, "int32": true, "int": true, "bool": true, "string": true, "float64": true, "[]string": true, "[]int64": true,
}

// resultRules maps {sqlcResultType, repositoryResultType} to the expression
// that converts a scalar query result, with %s standing for the result.
// Identical types need no entry.
var resultRules = map[[2]string]string{
	{"int64", "bool"}:  "%s != 0",
	{"int32", "int"}:   "int(%s)",
	{"int64", "int"}:   "int(%s)",
	{"int32", "int64"}: "int64(%s)",
	{"int64", "int32"}: "int32(%s)",
}

// zeroValue renders the zero value of a scalarResults type.
func zeroValue(typ string) string {
	switch {
	case typ == "bool":
		return "false"
	case typ == "string":
		return `""`
	case strings.HasPrefix(typ, "[]"):
		return "nil"
	}
	return "0"
}

// renderer renders candidate bodies for one dialect. pkgFuncs holds the
// package-level functions of the adapter's hand-written files, which tells
// the renderer whether a hand-written mapper exists for a domain type.
type renderer struct {
	d        dialect
	gen      map[string]map[string]string
	queries  map[string]querySig
	pkgFuncs map[string]bool
}

// candidates renders every shape a repository method fits, or nil when it fits
// none: no sqlc query of that name, an argument with no conversion, an
// argument that matches no Params field by name, or a result pairing the
// generator does not know.
func (r renderer) candidates(name string, sig methodSig) []candidate {
	if len(sig.params) == 0 || sig.params[0].typ != "context.Context" {
		return nil
	}
	qname := name
	if alias, ok := queryAliases[name]; ok {
		qname = alias
	}
	q, ok := r.queries[qname]
	if !ok {
		return nil
	}
	names := make([]string, len(sig.params))
	for i, p := range sig.params {
		names[i] = p.name
		if names[i] == "" {
			names[i] = fmt.Sprintf("a%d", i)
		}
	}
	call, ok := r.callArgs(qname, sig, names, q)
	if !ok {
		return nil
	}
	head := fmt.Sprintf("func (a *%s) %s(%s)", r.d.adapterType, name, groupedDecls(sig.params, names))
	invoke := fmt.Sprintf("a.queries.%s(%s)", qname, call)
	wraps := r.errWraps(name, qname, sig, names)
	guard := r.d.rowLocks[qname]
	res := sig.results
	switch {
	case len(res) == 1 && res[0] == "error":
		switch {
		case len(q.results) == 1 && q.results[0] == "error":
			return execCandidates(head, invoke, wraps, guard)
		case len(q.results) == 2 && q.results[0] == "int64" && q.results[1] == "error":
			return affectedCandidates(head, invoke, wraps, guard)
		}
	case len(res) == 2 && res[1] == "error" && len(q.results) == 2 && q.results[1] == "error":
		dom, row := res[0], q.results[0]
		switch {
		case strings.HasPrefix(dom, "*") && isDomainType(dom[1:]) && !strings.HasPrefix(row, "[]"):
			return r.rowCandidates(head, invoke, dom[1:], row, wraps, guard)
		case strings.HasPrefix(dom, "[]") && isDomainType(dom[2:]) && strings.HasPrefix(row, "[]"):
			return r.sliceCandidates(head, invoke, dom[2:], row[2:], wraps, guard)
		case scalarResults[dom]:
			out := scalarCandidates(head, invoke, dom, row, wraps, guard)
			if dom == "bool" && row == "int64" {
				out = append(out, affectedBoolCandidates(head, invoke, wraps, guard)...)
			}
			return out
		}
	}
	return nil
}

// isDomainType reports whether an interface result type is an unqualified
// struct name, which the Repository interface resolves in package repository.
func isDomainType(t string) bool {
	return t != "" && !strings.ContainsAny(t, ".*[]") && strings.ToUpper(t[:1]) == t[:1]
}

// callArgs renders the arguments passed to the sqlc query: either every
// repository parameter converted positionally, or a <Query>Params literal whose
// fields are matched to the parameters by name. Field names and count must
// match the repository signature, which keeps two same-typed arguments from
// being swapped silently.
func (r renderer) callArgs(qname string, sig methodSig, names []string, q querySig) (string, bool) {
	args := []string{names[0]}
	if len(q.params) == 1 && q.params[0].typ == qname+"Params" {
		pf, ok := r.gen[qname+"Params"]
		if !ok || len(names)-1 != len(pf) {
			return "", false
		}
		fieldByNorm := make(map[string]string, len(pf))
		for fn := range pf {
			fieldByNorm[strings.ToLower(fn)] = fn
		}
		var assigns []string
		for j := 1; j < len(names); j++ {
			field, ok := fieldByNorm[strings.ToLower(names[j])]
			if !ok {
				return "", false
			}
			val, ok := convertArg(sig.params[j].typ, pf[field], names[j])
			if !ok {
				return "", false
			}
			assigns = append(assigns, field+": "+val)
		}
		args = append(args, fmt.Sprintf("%s.%sParams{%s}", r.d.genPkg, qname, strings.Join(assigns, ", ")))
		return strings.Join(args, ", "), true
	}
	if len(q.params) != len(names)-1 {
		return "", false
	}
	for j := 1; j < len(names); j++ {
		val, ok := convertArg(sig.params[j].typ, q.params[j-1].typ, names[j])
		if !ok {
			return "", false
		}
		args = append(args, val)
	}
	return strings.Join(args, ", "), true
}

// errWraps lists the expressions of err the adapters return from a failed
// query: bare, mapped to the repository sentinels, or wrapped with a message
// naming the dialect and the action, optionally with the first argument.
func (r renderer) errWraps(name, qname string, sig methodSig, names []string) []string {
	phrases := actionPhrases(name)
	if qname != name {
		phrases = append(phrases, actionPhrases(qname)...)
	}
	type verbArg struct{ verb, arg string }
	verbs := []verbArg{{}}
	if len(sig.params) > 1 {
		switch sig.params[1].typ {
		case "string":
			verbs = append(verbs, verbArg{" %s", names[1]}, verbArg{" %q", names[1]})
		case "int64", "int", "int32":
			verbs = append(verbs, verbArg{" %d", names[1]})
		}
	}
	inners := []string{"err", "mapErr(err)"}
	out := append([]string(nil), inners...)
	for _, ph := range phrases {
		for _, v := range verbs {
			for _, inner := range inners {
				args := inner
				if v.arg != "" {
					args = v.arg + ", " + inner
				}
				out = append(out, fmt.Sprintf("fmt.Errorf(%q, %s)", r.d.name+" "+ph+v.verb+": %w", args))
			}
		}
	}
	return out
}

// guardSrc renders the transaction guard for row-locking queries; zero is the
// value returned alongside the error, or "" for error-only methods.
func guardSrc(guard bool, zero string) string {
	if !guard {
		return ""
	}
	ret := "repository.ErrNoTransaction"
	if zero != "" {
		ret = zero + ", " + ret
	}
	return "\tif !a.inTransaction() {\n\t\treturn " + ret + "\n\t}\n"
}

func ifErrReturn(invoke, ret string) string {
	return "\tif err := " + invoke + "; err != nil {\n\t\treturn " + ret + "\n\t}\n\treturn nil\n}\n"
}

// execCandidates renders an error-only method over an exec query.
func execCandidates(head, invoke string, wraps []string, guard bool) []candidate {
	h := head + " error {\n" + guardSrc(guard, "")
	out := []candidate{
		{emit: h + "\treturn " + invoke + "\n}\n", match: []string{h + ifErrReturn(invoke, "err")}},
		{emit: h + "\treturn mapErr(" + invoke + ")\n}\n", match: []string{h + ifErrReturn(invoke, "mapErr(err)")}},
	}
	for _, w := range wraps {
		if w == "err" || w == "mapErr(err)" {
			continue
		}
		out = append(out, candidate{emit: h + ifErrReturn(invoke, w)})
	}
	return out
}

// affectedCandidates renders an error-only method over an execrows query:
// either the execution-ownership helper or a zero-rows check that reports
// ErrNotFound.
func affectedCandidates(head, invoke string, wraps []string, guard bool) []candidate {
	h := head + " error {\n" + guardSrc(guard, "")
	out := []candidate{{emit: h + "\treturn executionAffected(" + invoke + ")\n}\n"}}
	for _, w := range wraps {
		out = append(out, candidate{emit: h + "\tn, err := " + invoke + "\n\tif err != nil {\n\t\treturn " + w +
			"\n\t}\n\tif n == 0 {\n\t\treturn repository.ErrNotFound\n\t}\n\treturn nil\n}\n"})
	}
	return out
}

// scalarCandidates renders a (S, error) method over a query returning a
// scalar: the query result is returned unchanged or converted by resultRules,
// with every error style. An unconverted result with no guard is emitted as a
// single return of the query, which also accepts the tuple spelled out; the
// mapErr style also accepts "return v, mapErr(err)", which is the same
// because sqlc yields the zero value alongside an error and mapErr(nil) is
// nil.
func scalarCandidates(head, invoke, dom, row string, wraps []string, guard bool) []candidate {
	conv := "v"
	if dom != row {
		tmpl, ok := resultRules[[2]string{row, dom}]
		if !ok {
			return nil
		}
		conv = fmt.Sprintf(tmpl, "v")
	}
	zero := zeroValue(dom)
	h := head + " (" + dom + ", error) {\n" + guardSrc(guard, zero)
	var out []candidate
	for _, w := range wraps {
		body := "\tv, err := " + invoke + "\n\tif err != nil {\n\t\treturn " + zero + ", " + w + "\n\t}\n\treturn " + conv + ", nil\n}\n"
		c := candidate{emit: h + body}
		switch {
		case w == "err" && conv == "v" && !guard:
			c = candidate{emit: h + "\treturn " + invoke + "\n}\n", match: []string{h + body, h + "\tv, err := " + invoke + "\n\treturn v, err\n}\n"}}
		case w == "mapErr(err)" && conv == "v":
			c.match = []string{h + "\tv, err := " + invoke + "\n\treturn v, mapErr(err)\n}\n"}
		}
		out = append(out, c)
	}
	return out
}

// affectedBoolCandidates renders a (bool, error) method over an execrows
// query, reporting whether any row changed.
func affectedBoolCandidates(head, invoke string, wraps []string, guard bool) []candidate {
	h := head + " (bool, error) {\n" + guardSrc(guard, "false")
	var out []candidate
	for _, w := range wraps {
		out = append(out, candidate{emit: h + "\taffected, err := " + invoke + "\n\tif err != nil {\n\t\treturn false, " + w +
			"\n\t}\n\treturn affected > 0, nil\n}\n"})
	}
	return out
}

// mapperName returns the mapper suffix for a domain type: the genTypes name
// when the mapper is generated, otherwise the type itself.
func mapperName(dom string) string {
	if spec, ok := mapperSpec(dom); ok {
		return spec.name
	}
	return dom
}

// rowCandidates renders a (*T, error) method over a one-row query mapped by
// <dialect><T>ToDomain, which must be generated or hand-written.
func (r renderer) rowCandidates(head, invoke, dom, row string, wraps []string, guard bool) []candidate {
	spec, generated := mapperSpec(dom)
	if generated && spec.rowType() != row {
		return nil
	}
	mapper := r.d.name + mapperName(dom) + "ToDomain"
	if !generated && !r.pkgFuncs[mapper] {
		return nil
	}
	h := head + " (*repository." + dom + ", error) {\n" + guardSrc(guard, "nil")
	var out []candidate
	for _, w := range wraps {
		out = append(out, candidate{emit: h + "\trow, err := " + invoke + "\n\tif err != nil {\n\t\treturn nil, " + w +
			"\n\t}\n\treturn " + mapper + "(row), nil\n}\n"})
	}
	return out
}

// sliceCandidates renders a ([]T, error) method over a many-rows query. It
// calls the plural mapper when one exists and otherwise spells the
// make-and-loop over the single-row mapper inline. When the plural mapper is
// generated, its body is that same loop, so the inline spelling is accepted
// as a match.
func (r renderer) sliceCandidates(head, invoke, dom, row string, wraps []string, guard bool) []candidate {
	spec, generated := mapperSpec(dom)
	if generated && spec.rowType() != row {
		return nil
	}
	single := r.d.name + mapperName(dom) + "ToDomain"
	plural := r.d.name + dom + "sToDomain"
	if generated {
		plural = r.d.name + spec.pluralName() + "ToDomain"
	}
	loop := "\tout := make([]repository." + dom + ", len(rows))\n\tfor i, r := range rows {\n\t\tout[i] = *" + single + "(r)\n\t}\n\treturn out, nil\n}\n"
	var ret, alt string
	switch {
	case generated && spec.slice:
		ret, alt = "\treturn "+plural+"(rows), nil\n}\n", loop
	case !generated && r.pkgFuncs[plural]:
		ret = "\treturn " + plural + "(rows), nil\n}\n"
	case generated || r.pkgFuncs[single]:
		ret = loop
	default:
		return nil
	}
	h := head + " ([]repository." + dom + ", error) {\n" + guardSrc(guard, "nil")
	var out []candidate
	for _, w := range wraps {
		pre := h + "\trows, err := " + invoke + "\n\tif err != nil {\n\t\treturn nil, " + w + "\n\t}\n"
		c := candidate{emit: pre + ret}
		if alt != "" {
			c.match = []string{pre + alt}
		}
		out = append(out, c)
	}
	return out
}

// groupedDecls renders parameter declarations, grouping consecutive params that
// share a type ("limit, offset int") the way hand-written signatures do.
func groupedDecls(params []param, names []string) string {
	var groups []string
	for i := 0; i < len(params); {
		j := i
		for j+1 < len(params) && params[j+1].typ == params[i].typ {
			j++
		}
		groups = append(groups, strings.Join(names[i:j+1], ", ")+" "+params[i].typ)
		i = j + 1
	}
	return strings.Join(groups, ", ")
}
