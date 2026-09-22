package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// explain reports, for every hand-written interface method of the renderer's
// dialect, why the generator leaves it alone, so a harvesting round starts
// from facts rather than from reading bodies.
func (r renderer) explain(methods map[string]methodSig, root string) ([]string, error) {
	hand, pkgFuncs, err := r.handWrittenMethods(filepath.Join(root, r.d.dir))
	if err != nil {
		return nil, err
	}
	r.pkgFuncs = pkgFuncs
	names := make([]string, 0, len(hand))
	for name := range hand {
		if _, ok := methods[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		reason, err := r.reason(name, methods[name], hand[name].norm)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, fmt.Sprintf("%s %s: %s", r.d.name, name, reason))
	}
	return out, nil
}

// reason classifies one hand-written method against the generator's shapes,
// from the first obstacle: denied, no query, arguments that do not map, a
// result pairing with no shape, or a body that differs from every shape.
func (r renderer) reason(name string, sig methodSig, norm string) (string, error) {
	if r.cfg.denied(name) {
		return "denied: " + r.cfg.Deny[name], nil
	}
	if len(sig.params) == 0 || sig.params[0].typ != "context.Context" {
		return "no context parameter", nil
	}
	qname := r.cfg.alias(name)
	q, ok := r.queries[qname]
	if !ok {
		return "no sqlc query named " + qname, nil
	}
	cands := r.candidates(name, sig)
	if len(cands) == 0 {
		names := make([]string, len(sig.params))
		for i, p := range sig.params {
			names[i] = p.name
		}
		if _, ok := r.callArgs(qname, sig, names, q); !ok {
			return "arguments do not map onto " + r.describeParams(qname, q), nil
		}
		return fmt.Sprintf("no shape returns (%s) from (%s)", strings.Join(sig.results, ", "), strings.Join(q.results, ", ")), nil
	}
	c, err := r.matchCandidate(norm, cands, true)
	if err != nil {
		return "", err
	}
	if c != nil {
		return "matches a shape; the next generating run harvests it", nil
	}
	return "body differs from every generated shape", nil
}

// describeParams names what a query takes: its Params fields or its
// positional parameters.
func (r renderer) describeParams(qname string, q querySig) string {
	if len(q.params) == 1 && q.params[0].typ == qname+"Params" {
		fields := make([]string, 0, len(r.gen[qname+"Params"]))
		for f, t := range r.gen[qname+"Params"] {
			fields = append(fields, f+" "+t)
		}
		sort.Strings(fields)
		return qname + "Params{" + strings.Join(fields, ", ") + "}"
	}
	ps := make([]string, len(q.params))
	for i, p := range q.params {
		ps[i] = p.name + " " + p.typ
	}
	return "(" + strings.Join(ps, ", ") + ")"
}
