package main

import (
	"log/slog"
	"sort"
	"strings"
)

// BuildEventSubModel turns the scraped reference + subscription types into the
// generator's view: typed condition + event structs and the (type, version) →
// struct dispatch data used by generated UnmarshalJSON factories.
func BuildEventSubModel(ref *EventSubReference, subs []EventSubSubscriptionType, model *templateModel, log *slog.Logger) {
	condAnchors := usedAnchors(subs, func(s EventSubSubscriptionType) string { return s.ConditionAnchor })
	evtAnchors := usedAnchors(subs, func(s EventSubSubscriptionType) string { return s.EventAnchor })

	condNames, condTypes := emitSchemaStructs(ref.Conditions, condAnchors, "Condition", model, log)
	evtNames, evtTypes := emitSchemaStructs(ref.Events, evtAnchors, "Event", model, log)

	model.EventSubConditions = condTypes
	model.EventSubEvents = evtTypes
	model.EventSubDispatch = buildDispatch(subs, condNames, evtNames)
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
		for _, f := range schema.Fields {
			fm, err := toEventSubFieldModel(f, log)
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

// toEventSubFieldModel is the EventSub-specific cousin of toFieldModel.
// Currently identical (same naming + typemap rules) but kept separate so
// EventSub-specific adjustments (e.g. nested object promotion) can land
// without touching the Helix struct path.
func toEventSubFieldModel(f FieldSchema, log *slog.Logger) (fieldModel, error) {
	// Reuse the Helix flattening logic — nested Object/Object[] → any for now.
	// Nested named structs for EventSub payloads are a later enhancement.
	goType := GoType(f, "")
	if goType == "any" || goType == "[]any" {
		log.Warn("generator: eventsub unknown type", "name", f.Name, "type", f.Type)
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
