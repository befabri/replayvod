package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// singularAnchors are schema anchors that end in 's' but already name a single
// instance — the generic `singularize` rule would mis-strip the trailing 's'.
// Add entries here when a real anchor produces a broken struct name; mechanical
// extension based on observed misses, not speculative.
var singularAnchors = map[string]bool{
	"max_per_stream":          true,
	"max_per_user_per_stream": true,
	"global_cooldown":         true, // safe no-op but documents intent
	"bits_voting":             true,
	"channel_points_voting":   true,
}

// schemaGoName returns the Go struct name for a NamedSchemas anchor.
// Plural anchors are singularized (outcomes → Outcome); single-instance
// anchors (image, reward, max_per_stream…) keep their form.
func schemaGoName(anchor string) string {
	if singularAnchors[anchor] {
		return PascalCase(anchor)
	}
	return PascalCase(singularize(anchor))
}

// isPluralAnchor reports whether the anchor describes a plural concept
// (driving array-vs-scalar emission for fields referencing it).
func isPluralAnchor(anchor string) bool {
	if singularAnchors[anchor] {
		return false
	}
	return singularize(anchor) != anchor
}

// BuildEventSubModel turns the scraped reference + subscription types into the
// generator's view: typed condition + event structs and the (type, version) →
// struct dispatch data used by generated UnmarshalJSON factories.
//
// Named schemas referenced by condition/event fields (image, outcomes, reward…)
// are emitted transitively — only anchors actually reached by a walk through
// condition/event fields get materialized, keeping the set tight.
func BuildEventSubModel(ref *EventSubReference, subs []EventSubSubscriptionType, model *templateModel, log *slog.Logger) {
	condAnchors := usedAnchors(subs, func(s EventSubSubscriptionType) string { return s.ConditionAnchor })
	evtAnchors := usedAnchors(subs, func(s EventSubSubscriptionType) string { return s.EventAnchor })

	// Reachable NamedSchemas accumulate across the three emitSchemaStructs
	// calls. Each toEventSubFieldModel call may enqueue anchors by writing to
	// this set; a post-pass emits them as additional nested types.
	reached := map[string]bool{}

	resolver := &namedSchemaResolver{ref: ref, reached: reached}

	condNames, condTypes := emitSchemaStructs(ref.Conditions, condAnchors, "Condition", model, resolver, log)
	evtNames, evtTypes := emitSchemaStructs(ref.Events, evtAnchors, "Event", model, resolver, log)

	// Fixed-point walk: expand reached set until no new anchors are discovered.
	// A named schema's fields may reference other named schemas — emit them
	// into the Events slice (Nested=true) as structs alongside.
	namedTypes := emitReachedNamedSchemas(ref, resolver, model, log)

	model.EventSubConditions = condTypes
	model.EventSubEvents = append(evtTypes, namedTypes...)
	model.EventSubDispatch = buildDispatch(subs, condNames, evtNames)
}

// namedSchemaResolver lets toEventSubFieldModel look up whether an unknown
// type string matches a NamedSchemas anchor. Anchors found during resolution
// are added to `reached`; a post-pass in BuildEventSubModel emits them.
type namedSchemaResolver struct {
	ref     *EventSubReference
	reached map[string]bool
}

// resolve looks up `typeStr` against NamedSchemas, normalizing case and
// underscores. Returns ("", false) if not found. Otherwise returns the emitted
// Go type string ("[]Outcome" for plurals, "Image" for singulars) and marks
// the anchor reached so it gets emitted later.
func (r *namedSchemaResolver) resolve(typeStr string) (string, bool) {
	if r == nil || r.ref == nil {
		return "", false
	}
	key := typeAnchorReplacer.Replace(strings.ToLower(strings.TrimSpace(typeStr)))
	if _, ok := r.ref.NamedSchemas[key]; !ok {
		return "", false
	}
	r.reached[key] = true
	name := schemaGoName(key)
	if isPluralAnchor(key) {
		return "[]" + name, true
	}
	return name, true
}

// emitReachedNamedSchemas walks the reached set to a fixed point — each named
// schema's fields may reference further named schemas — and returns a slice of
// eventSubTypeModel entries (Nested=true) for the final set, sorted by Go name
// for deterministic output.
func emitReachedNamedSchemas(ref *EventSubReference, resolver *namedSchemaResolver, model *templateModel, log *slog.Logger) []eventSubTypeModel {
	emitted := map[string]bool{}
	var out []eventSubTypeModel
	// Seed with currently-reached anchors; may grow during the walk.
	for progress := true; progress; {
		progress = false
		// Deterministic iteration: take a sorted snapshot of reached.
		pending := make([]string, 0, len(resolver.reached))
		for a := range resolver.reached {
			if !emitted[a] {
				pending = append(pending, a)
			}
		}
		sort.Strings(pending)
		for _, anchor := range pending {
			schema, ok := ref.NamedSchemas[anchor]
			if !ok {
				continue
			}
			emitted[anchor] = true
			progress = true

			name := schemaGoName(anchor)
			tm := eventSubTypeModel{GoName: name, AnchorID: anchor, Nested: true}
			// Children of a named schema can themselves reference other named
			// schemas — recursing through toEventSubFieldModel triggers
			// resolver.resolve, growing the reached set.
			localEmit := map[string]bool{name: true}
			for _, f := range schema.Fields {
				fm, err := toEventSubFieldModel(name, f, &out, localEmit, model, resolver, log)
				if err != nil {
					log.Warn("eventsub: named-schema field conversion", "anchor", anchor, "field", f.Name, "err", err)
					continue
				}
				tm.Fields = append(tm.Fields, fm)
				if strings.Contains(fm.GoType, "time.Time") {
					model.ImportTime = true
				}
			}
			out = append(out, tm)
		}
	}
	return out
}

// usedAnchors collects every anchor key referenced by at least one subscription
// type via the given getter. Keeps the emitted struct set tight (we won't
// generate types for reference-page sections nobody subscribes to).
func usedAnchors(subs []EventSubSubscriptionType, get func(EventSubSubscriptionType) string) map[string]bool {
	out := map[string]bool{}
	for _, s := range subs {
		k := get(s)
		if k != "" {
			out[k] = true
		}
	}
	return out
}

// emitSchemaStructs translates every anchor in `used` into a Go struct via
// its fields in `schemas`, appends the struct to model.EventSubBodyTypes, and
// returns (anchor → GoName) plus the sorted list of type models for the template.
func emitSchemaStructs(
	schemas map[string]EventSubSchema,
	used map[string]bool,
	suffix string,
	model *templateModel,
	resolver *namedSchemaResolver,
	log *slog.Logger,
) (map[string]string, []eventSubTypeModel) {
	// Deterministic iteration.
	anchors := make([]string, 0, len(used))
	for a := range used {
		anchors = append(anchors, a)
	}
	sort.Strings(anchors)

	names := map[string]string{}
	var out []eventSubTypeModel
	for _, anchor := range anchors {
		schema, ok := schemas[anchor]
		if !ok {
			log.Warn("eventsub: used anchor missing from schemas", "anchor", anchor, "suffix", suffix)
			continue
		}
		name := anchorToGoName(anchor, suffix)
		if _, dupe := findEventSubTypeByName(out, name); dupe {
			log.Warn("eventsub: duplicate Go name; skipping", "anchor", anchor, "name", name)
			continue
		}
		names[anchor] = name

		tm := eventSubTypeModel{
			GoName:   name,
			AnchorID: anchor,
		}
		emittedNested := map[string]bool{name: true}
		for _, f := range schema.Fields {
			fm, err := toEventSubFieldModel(name, f, &out, emittedNested, model, resolver, log)
			if err != nil {
				log.Warn("eventsub: field conversion", "anchor", anchor, "field", f.Name, "err", err)
				continue
			}
			tm.Fields = append(tm.Fields, fm)
			if strings.Contains(fm.GoType, "time.Time") {
				model.ImportTime = true
			}
		}
		out = append(out, tm)
	}
	return names, out
}

func findEventSubTypeByName(list []eventSubTypeModel, name string) (*eventSubTypeModel, bool) {
	for i := range list {
		if list[i].GoName == name {
			return &list[i], true
		}
	}
	return nil, false
}

// anchorToGoName turns an anchor into a Go struct name.
// "channel-follow-condition" → "ChannelFollowCondition".
// "shield-mode" (for events) → "ShieldModeEvent" (suffix appended).
// "automod-message-hold-event-v2" (for events) → "AutomodMessageHoldEventV2"
// (no duplicate suffix since the word already appears).
func anchorToGoName(anchor, suffix string) string {
	name := PascalCase(anchor)
	if strings.Contains(name, suffix) {
		return name
	}
	return name + suffix
}

// buildDispatch assembles the (type, version) → struct-name factory cases for
// both conditions and events. Sorted for deterministic output.
func buildDispatch(subs []EventSubSubscriptionType, condNames, evtNames map[string]string) eventSubDispatchModel {
	var d eventSubDispatchModel
	sorted := make([]EventSubSubscriptionType, len(subs))
	copy(sorted, subs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Type != sorted[j].Type {
			return sorted[i].Type < sorted[j].Type
		}
		return sorted[i].Version < sorted[j].Version
	})
	for _, s := range sorted {
		if name := condNames[s.ConditionAnchor]; name != "" {
			d.ConditionCases = append(d.ConditionCases, eventSubCase{
				TypeString: s.Type, Version: s.Version, GoName: name,
			})
		}
		if name := evtNames[s.EventAnchor]; name != "" {
			d.EventCases = append(d.EventCases, eventSubCase{
				TypeString: s.Type, Version: s.Version, GoName: name,
			})
		}
	}
	return d
}

// toEventSubFieldModel is the EventSub mirror of Helix's toStructFieldModel:
// fields with documented children are promoted to nested named structs, which
// are appended to `out` and referenced by generated Go type. Fields without
// children fall back to the flat typemap (may still be `any` for Twitch's
// cross-schema references like `reward` / `image` — see Tier 3).
func toEventSubFieldModel(
	parent string, f FieldSchema,
	out *[]eventSubTypeModel, emitted map[string]bool,
	model *templateModel, resolver *namedSchemaResolver, log *slog.Logger,
) (fieldModel, error) {
	goType := ""
	hasChildren := len(f.Children) > 0

	switch {
	case hasChildren:
		isArrayType := strings.HasSuffix(f.Type, "[]")
		nestedName := parent + PascalCase(f.Name)
		if isArrayType {
			nestedName = parent + PascalCase(singularize(f.Name))
		}
		if !emitted[nestedName] {
			emitted[nestedName] = true
			nested := eventSubTypeModel{GoName: nestedName, Nested: true}
			for _, child := range f.Children {
				cfm, err := toEventSubFieldModel(nestedName, child, out, emitted, model, resolver, log)
				if err != nil {
					return fieldModel{}, fmt.Errorf("nested field %q: %w", child.Name, err)
				}
				nested.Fields = append(nested.Fields, cfm)
				if strings.Contains(cfm.GoType, "time.Time") {
					model.ImportTime = true
				}
			}
			*out = append(*out, nested)
		}
		if isArrayType {
			goType = "[]" + nestedName
		} else {
			goType = nestedName
		}
	default:
		// No inline children — try cross-schema resolution first, fall back to flat typemap.
		if resolved, ok := resolver.resolve(f.Type); ok {
			goType = resolved
		} else {
			goType = GoType(f, "")
			if goType == "any" || goType == "[]any" {
				log.Warn("generator: eventsub unknown type", "name", f.Name, "type", f.Type, "parent", parent)
			}
		}
	}

	omitEmpty := f.Required == nil || !*f.Required
	return fieldModel{
		GoName:     PascalCase(f.Name),
		GoType:     goType,
		JSONName:   f.Name,
		OmitEmpty:  omitEmpty,
		Deprecated: isDeprecatedField(f.Description),
	}, nil
}
