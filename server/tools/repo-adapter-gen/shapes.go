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

// scalarResults are the interface result types the scalar shape can return:
// a query result passed through unchanged or through the config's result
// rules. They are the types zeroValue can render.
var scalarResults = map[string]bool{
	"int64": true, "int32": true, "int": true, "bool": true, "string": true, "float64": true, "[]string": true, "[]int64": true,
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

// renderer renders candidate bodies for one dialect under the project's
// config. pkgFuncs holds the package-level functions of the adapter's
// hand-written files, which tells the renderer whether a hand-written mapper
// exists for a domain type.
type renderer struct {
	cfg      *config
	d        dialect
	domain   map[string]map[string]string
	gen      map[string]map[string]string
	queries  map[string]querySig
	pkgFuncs map[string]bool
	values   map[string][]param
}

// domainType qualifies a domain type for adapter code.
func (r renderer) domainType(t string) string {
	return r.cfg.Domain.Package + "." + t
}

// mapperName names the mapper for a generated spec name or a hand-written
// domain type.
func (r renderer) mapperName(name string) string {
	return r.d.name + name + r.cfg.Conventions.MapperSuffix
}

// mappedErr renders the configured error mapper applied to err.
func (r renderer) mappedErr() string {
	return r.cfg.Conventions.ErrorMapper + "(err)"
}

// candidates renders every shape an interface method fits, or nil when it
// fits none: no sqlc query of that name, an argument with no conversion, an
// argument that matches no Params field by name, or a result pairing the
// generator does not know.
func (r renderer) candidates(name string, sig methodSig) []candidate {
	if len(sig.params) == 0 || sig.params[0].typ != "context.Context" {
		return nil
	}
	qname := r.cfg.alias(name)
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
	declParams := append([]param(nil), sig.params...)
	for i, p := range declParams {
		if _, ok := r.values[p.typ]; ok {
			declParams[i].typ = r.domainType(p.typ)
		}
	}
	head := fmt.Sprintf("func (a *%s) %s(%s)", r.d.adapterType, name, groupedDecls(declParams, names))
	invoke := fmt.Sprintf("a.%s.%s(%s)", r.cfg.Conventions.QueriesField, qname, call)
	wraps := r.errWraps(name, qname, sig, names)
	g := guards{values: r.valueParams(sig), tx: r.d.rowLocks[qname], empty: sliceParam(sig, names)}
	res := sig.results
	switch {
	case len(res) == 1 && res[0] == "error":
		switch {
		case len(q.results) == 1 && q.results[0] == "error":
			return r.execCandidates(head, invoke, wraps, g)
		case len(q.results) == 2 && q.results[0] == "int64" && q.results[1] == "error":
			return r.affectedCandidates(head, invoke, wraps, g)
		case len(q.results) == 2 && q.results[1] == "error":
			return r.discardCandidates(head, invoke, wraps, g)
		}
	case len(res) == 2 && res[1] == "error" && len(q.results) == 2 && q.results[1] == "error":
		dom, row := res[0], q.results[0]
		switch {
		case strings.HasPrefix(dom, "*") && isDomainType(dom[1:]) && !strings.HasPrefix(row, "[]"):
			return r.rowCandidates(head, invoke, dom[1:], row, wraps, g)
		case strings.HasPrefix(dom, "[]") && isDomainType(dom[2:]) && strings.HasPrefix(row, "[]"):
			return r.sliceCandidates(head, invoke, dom[2:], row[2:], wraps, g)
		case scalarResults[dom]:
			out := r.scalarCandidates(head, invoke, dom, row, wraps, g)
			if dom == "bool" && row == "int64" {
				out = append(out, r.affectedBoolCandidates(head, invoke, wraps, g)...)
			}
			return out
		}
	}
	return nil
}

// sliceParam names the sole slice parameter of sig, or "" when there is none
// or more than one.
func sliceParam(sig methodSig, names []string) string {
	out := ""
	for i, p := range sig.params[1:] {
		if strings.HasPrefix(p.typ, "[]") {
			if out != "" {
				return ""
			}
			out = names[i+1]
		}
	}
	return out
}

// isDomainType reports whether an interface result type is an unqualified
// struct name, which the interface resolves in the domain package.
func isDomainType(t string) bool {
	return t != "" && !strings.ContainsAny(t, ".*[]") && strings.ToUpper(t[:1]) == t[:1]
}

// callArgs renders scalar arguments and value-object accessors for a sqlc
// query. Params fields match by name. Scalar-only query arguments retain their
// positional convention; expanded value objects also require names to match
// when more than one positional argument exists, so accessor ordering can
// never swap two same-typed query arguments.
func (r renderer) callArgs(qname string, sig methodSig, names []string, q querySig) (string, bool) {
	args := []string{names[0]}
	inputs := r.queryArgs(sig, names)
	seen := map[string]bool{}
	for _, input := range inputs {
		key := strings.ToLower(input.name)
		if seen[key] {
			return "", false
		}
		seen[key] = true
	}
	if len(q.params) == 1 && q.params[0].typ == qname+"Params" {
		pf, ok := r.gen[qname+"Params"]
		if !ok || len(inputs) != len(pf) {
			return "", false
		}
		fieldByNorm := make(map[string]string, len(pf))
		for fn := range pf {
			fieldByNorm[strings.ToLower(fn)] = fn
		}
		var assigns []string
		for _, input := range inputs {
			field, ok := fieldByNorm[strings.ToLower(input.name)]
			if !ok {
				return "", false
			}
			val, ok := r.cfg.convertArg(input.typ, pf[field], input.expr)
			if !ok {
				return "", false
			}
			assigns = append(assigns, field+": "+val)
		}
		args = append(args, fmt.Sprintf("%s.%sParams{%s}", r.d.genPkg, qname, strings.Join(assigns, ", ")))
		return strings.Join(args, ", "), true
	}
	if len(q.params) != len(inputs) {
		return "", false
	}
	if len(r.valueParams(sig)) > 0 && len(inputs) > 1 {
		byName := make(map[string]queryArg, len(inputs))
		for _, input := range inputs {
			byName[strings.ToLower(input.name)] = input
		}
		ordered := make([]queryArg, len(inputs))
		for i, p := range q.params {
			input, ok := byName[strings.ToLower(p.name)]
			if !ok {
				return "", false
			}
			ordered[i] = input
		}
		inputs = ordered
	}
	for j, input := range inputs {
		val, ok := r.cfg.convertArg(input.typ, q.params[j].typ, input.expr)
		if !ok {
			return "", false
		}
		args = append(args, val)
	}
	return strings.Join(args, ", "), true
}

// errWraps lists the expressions of err the adapters return from a failed
// query: bare, through the error mapper, or wrapped with a message in the
// configured format naming the dialect and the action, optionally with the
// first argument.
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
	inners := []string{"err", r.mappedErr()}
	out := append([]string(nil), inners...)
	for _, ph := range phrases {
		for _, v := range verbs {
			msg := strings.NewReplacer("{dialect}", r.d.name, "{action}", ph+v.verb).Replace(r.cfg.Conventions.ErrorFormat)
			for _, inner := range inners {
				args := inner
				if v.arg != "" {
					args = v.arg + ", " + inner
				}
				out = append(out, fmt.Sprintf("fmt.Errorf(%q, %s)", msg, args))
			}
		}
	}
	return out
}

// guards are the checks a method runs before its query: value-object
// validation, then the transaction requirement of row-locking queries.
// Harvesting only accepts a body containing them, so adding a value parameter
// cannot silently generate an unchecked query or drop an existing check.
type guards struct {
	values []string
	tx     bool
	// empty names the sole slice parameter. Its empty value may short-circuit
	// the query as an alternative slice shape (see sliceCandidates).
	empty string
}

// guardSrc renders the guards; zero is the value returned beside the error,
// or "" for error-only methods.
func (r renderer) guardSrc(g guards, zero string) string {
	ret, txRet := "err", r.cfg.Conventions.NoTransaction
	if zero != "" {
		ret, txRet = zero+", err", zero+", "+txRet
	}
	var b strings.Builder
	for _, name := range g.values {
		fmt.Fprintf(&b, "\tif err := %s.Validate(); err != nil {\n\t\treturn %s\n\t}\n", name, ret)
	}
	if g.tx {
		b.WriteString("\tif !a." + r.cfg.Conventions.TransactionCheck + "() {\n\t\treturn " + txRet + "\n\t}\n")
	}
	return b.String()
}

func ifErrReturn(invoke, ret string) string {
	return "\tif err := " + invoke + "; err != nil {\n\t\treturn " + ret + "\n\t}\n\treturn nil\n}\n"
}

// execCandidates renders an error-only method over an exec query.
func (r renderer) execCandidates(head, invoke string, wraps []string, g guards) []candidate {
	h := head + " error {\n" + r.guardSrc(g, "")
	out := []candidate{
		{emit: h + "\treturn " + invoke + "\n}\n", match: []string{h + ifErrReturn(invoke, "err")}},
		{emit: h + "\treturn " + r.cfg.Conventions.ErrorMapper + "(" + invoke + ")\n}\n", match: []string{h + ifErrReturn(invoke, r.mappedErr())}},
	}
	for _, w := range wraps {
		if w == "err" || w == r.mappedErr() {
			continue
		}
		out = append(out, candidate{emit: h + ifErrReturn(invoke, w)})
	}
	return out
}

// affectedCandidates renders an error-only method over an execrows query:
// either the execution-ownership helper or a zero-rows check that reports
// the not-found sentinel.
func (r renderer) affectedCandidates(head, invoke string, wraps []string, g guards) []candidate {
	h := head + " error {\n" + r.guardSrc(g, "")
	out := []candidate{{emit: h + "\treturn " + r.cfg.Conventions.Affected + "(" + invoke + ")\n}\n"}}
	for _, w := range wraps {
		out = append(out, candidate{emit: h + "\tn, err := " + invoke + "\n\tif err != nil {\n\t\treturn " + w +
			"\n\t}\n\tif n == 0 {\n\t\treturn " + r.cfg.Conventions.NotFound + "\n\t}\n\treturn nil\n}\n"})
	}
	return out
}

// discardCandidates renders an error-only method over a query that returns a
// row the adapter ignores, such as an UPDATE ... RETURNING run for its effect.
// Wrapping styles keep the if form, since wrapping a nil error is not nil.
func (r renderer) discardCandidates(head, invoke string, wraps []string, g guards) []candidate {
	h := head + " error {\n" + r.guardSrc(g, "")
	var out []candidate
	for _, w := range wraps {
		ifForm := h + "\tif _, err := " + invoke + "; err != nil {\n\t\treturn " + w + "\n\t}\n\treturn nil\n}\n"
		if w == "err" || w == r.mappedErr() {
			out = append(out, candidate{emit: h + "\t_, err := " + invoke + "\n\treturn " + w + "\n}\n", match: []string{ifForm}})
			continue
		}
		out = append(out, candidate{emit: ifForm})
	}
	return out
}

// scalarCandidates renders a (S, error) method over a query returning a
// scalar: the query result is returned unchanged or converted by the result
// rules, with every error style. An unconverted result with no guard is
// emitted as a single return of the query, which also accepts the tuple
// spelled out; the mapped style also accepts "return v, mapErr(err)", which
// is the same because sqlc yields the zero value alongside an error and the
// mapper passes nil through.
func (r renderer) scalarCandidates(head, invoke, dom, row string, wraps []string, g guards) []candidate {
	conv := "v"
	if dom != row {
		tmpl, ok := r.cfg.resultConv(row, dom)
		if !ok {
			return nil
		}
		conv = fmt.Sprintf(tmpl, "v")
	}
	zero := zeroValue(dom)
	h := head + " (" + dom + ", error) {\n" + r.guardSrc(g, zero)
	var out []candidate
	for _, w := range wraps {
		body := "\tv, err := " + invoke + "\n\tif err != nil {\n\t\treturn " + zero + ", " + w + "\n\t}\n\treturn " + conv + ", nil\n}\n"
		c := candidate{emit: h + body}
		switch {
		case w == "err" && conv == "v" && r.guardSrc(g, zero) == "":
			c = candidate{emit: h + "\treturn " + invoke + "\n}\n", match: []string{h + body, h + "\tv, err := " + invoke + "\n\treturn v, err\n}\n"}}
		case w == r.mappedErr() && conv == "v":
			c.match = []string{h + "\tv, err := " + invoke + "\n\treturn v, " + r.mappedErr() + "\n}\n"}
		}
		out = append(out, c)
	}
	return out
}

// affectedBoolCandidates renders a (bool, error) method over an execrows
// query, reporting whether any row changed.
func (r renderer) affectedBoolCandidates(head, invoke string, wraps []string, g guards) []candidate {
	h := head + " (bool, error) {\n" + r.guardSrc(g, "false")
	var out []candidate
	for _, w := range wraps {
		out = append(out, candidate{emit: h + "\taffected, err := " + invoke + "\n\tif err != nil {\n\t\treturn false, " + w +
			"\n\t}\n\treturn affected > 0, nil\n}\n"})
	}
	return out
}

// rowCandidates renders a (*T, error) method over a one-row query mapped by
// the type's mapper, which must be generated or hand-written. When the mapper
// is generated, a body that spells its literal inline is accepted as a match.
func (r renderer) rowCandidates(head, invoke, dom, row string, wraps []string, g guards) []candidate {
	spec, generated := r.cfg.mapperSpec(dom, row)
	name := dom
	if generated {
		name = spec.name
	}
	mapper := r.mapperName(name)
	if !generated && !r.pkgFuncs[mapper] {
		return nil
	}
	var literal string
	if generated {
		if l, err := r.mapperLiteral(spec, "row"); err == nil {
			literal = "\treturn &" + l + ", nil\n}\n"
		}
	}
	h := head + " (*" + r.domainType(dom) + ", error) {\n" + r.guardSrc(g, "nil")
	var out []candidate
	for _, w := range wraps {
		pre := h + "\trow, err := " + invoke + "\n\tif err != nil {\n\t\treturn nil, " + w + "\n\t}\n"
		c := candidate{emit: pre + "\treturn " + mapper + "(row), nil\n}\n"}
		if literal != "" {
			c.match = []string{pre + literal}
		}
		out = append(out, c)
	}
	return out
}

// sliceCandidates renders a ([]T, error) method over a many-rows query. It
// calls the plural mapper when one exists and otherwise spells the
// make-and-loop over the single-row mapper inline. When the plural mapper is
// generated, its body is that same loop, so the inline spelling is accepted
// as a match. A sole slice argument may also be short-circuited when empty;
// both spellings are rendered and the harvested one is kept.
func (r renderer) sliceCandidates(head, invoke, dom, row string, wraps []string, g guards) []candidate {
	spec, generated := r.cfg.mapperSpec(dom, row)
	single := r.mapperName(dom)
	plural := r.mapperName(dom + "s")
	if generated {
		single = r.mapperName(spec.name)
		plural = r.mapperName(spec.pluralName())
	}
	elem := r.domainType(dom)
	loop := "\tout := make([]" + elem + ", len(rows))\n\tfor i, r := range rows {\n\t\tout[i] = *" + single + "(r)\n\t}\n\treturn out, nil\n}\n"
	var literalLoop string
	if generated {
		if literal, err := r.mapperLiteral(spec, "r"); err == nil {
			literalLoop = strings.Replace(loop, "*"+single+"(r)", literal, 1)
		}
	}
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
	h := head + " ([]" + elem + ", error) {\n" + r.guardSrc(g, "nil")
	heads := []string{h}
	if g.empty != "" {
		heads = append(heads, h+"\tif len("+g.empty+") == 0 {\n\t\treturn []"+elem+"{}, nil\n\t}\n")
	}
	var out []candidate
	for _, hd := range heads {
		for _, w := range wraps {
			pre := hd + "\trows, err := " + invoke + "\n\tif err != nil {\n\t\treturn nil, " + w + "\n\t}\n"
			c := candidate{emit: pre + ret}
			if alt != "" {
				c.match = []string{pre + alt}
			}
			if literalLoop != "" {
				c.match = append(c.match, pre+literalLoop)
			}
			out = append(out, c)
		}
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
