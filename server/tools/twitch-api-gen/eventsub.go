package main

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// EventSubReference is the scraped result of the eventsub-reference page,
// augmented with inline conditions found on the subscription-types page for
// subscription types that don't link out to a reference anchor.
type EventSubReference struct {
	// Conditions keyed by anchor id, e.g. "channel-follow-condition" or — for
	// inline conditions — by the master anchor on the subscription-types page.
	Conditions map[string]EventSubSchema
	// Events keyed by anchor id on the reference page, e.g. "channel-follow-event".
	Events map[string]EventSubSchema
	// NamedSchemas are shared data schemas referenced by their anchor name
	// from field type cells (e.g. a field with type "outcomes" points at
	// anchor "outcomes"). Keys are the anchor id. Emitted only when reachable
	// from a Condition or Event via field-type references — see BuildEventSubModel.
	NamedSchemas map[string]EventSubSchema
}

// EventSubSchema is one condition or event payload schema.
type EventSubSchema struct {
	AnchorID string        // e.g. "channel-follow-condition"
	Fields   []FieldSchema // parsed by the existing table parser
}

// EventSubSubscriptionType is one row in the eventsub-subscription-types master table.
type EventSubSubscriptionType struct {
	Type            string // e.g. "channel.follow"
	Version         string // e.g. "2"
	MasterAnchor    string // anchor on subscription-types page, e.g. "channelfollow"
	ConditionAnchor string // key into EventSubReference.Conditions
	EventAnchor     string // key into EventSubReference.Events (may be empty)
}

// ParseEventSubReference scrapes both EventSub pages and returns resolved
// condition + event schemas keyed by anchor id, plus the list of (type, version)
// subscriptions each pointing at their condition/event anchors.
func ParseEventSubReference(referenceDoc, subTypesDoc *goquery.Document, log *slog.Logger) (*EventSubReference, []EventSubSubscriptionType, error) {
	if log == nil {
		log = slog.Default()
	}

	ref := &EventSubReference{
		Conditions:   map[string]EventSubSchema{},
		Events:       map[string]EventSubSchema{},
		NamedSchemas: map[string]EventSubSchema{},
	}
	if err := parseReferenceSchemas(referenceDoc, ref); err != nil {
		return nil, nil, fmt.Errorf("parse eventsub reference: %w", err)
	}

	subs, err := parseSubscriptionTypes(subTypesDoc, ref, log)
	if err != nil {
		return nil, nil, fmt.Errorf("parse subscription types: %w", err)
	}

	sort.Slice(subs, func(i, j int) bool {
		if subs[i].Type != subs[j].Type {
			return subs[i].Type < subs[j].Type
		}
		return subs[i].Version < subs[j].Version
	})
	return ref, subs, nil
}

// parseReferenceSchemas walks every `<h3 id>` ending in `-condition` or `-event`
// on the reference page and turns the next sibling `<table>` into a FieldSchema tree.
//
// ErrNotSchemaTable is expected for many tables on the page (non-schema tables
// like "Possible values" or intro prose tables) and is treated as "skip, move on."
// Any other ParseTable error is a real parse failure — likely a Twitch format
// change we need to audit — and aborts generation so the bug is visible.
func parseReferenceSchemas(doc *goquery.Document, ref *EventSubReference) error {
	var walkErr error
	doc.Find("h2[id], h3[id]").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		id, _ := h.Attr("id")
		if id == "" {
			return true
		}
		table := firstTableBeforeNextHeading(h)
		if table.Length() == 0 {
			return true
		}
		fields, err := ParseTable(table)
		if err != nil {
			if errors.Is(err, ErrNotSchemaTable) {
				return true // expected: this table isn't a schema
			}
			walkErr = fmt.Errorf("eventsub schema %q: %w", id, err)
			return false
		}
		setRequiredDefault(fields, true, nil)
		schema := EventSubSchema{AnchorID: id, Fields: fields}
		switch {
		case strings.HasSuffix(id, "-condition"):
			ref.Conditions[id] = schema
		case isEventAnchor(id):
			// Plain `-event` suffix OR versioned `-event-v{N}` suffix. Twitch
			// has separate anchors per version for some types whose event shape
			// changed (automod.message.hold v1 vs v2, channel.moderate v1 vs v2).
			ref.Events[id] = schema
		default:
			// Shared data schemas (image, outcomes, reward, max-per-stream, …).
			// Emitted lazily when a Condition/Event field actually references one.
			ref.NamedSchemas[id] = schema
		}
		return true
	})
	return walkErr
}

// eventAnchorSuffixRe matches versioned event anchor suffixes like
// `-event-v2` that the subscription-types page links for v2 variants of
// automod.message.hold, automod.message.update, and channel.moderate.
var eventAnchorSuffixRe = regexp.MustCompile(`-event-v\d+$`)

// isEventAnchor reports whether an anchor id is a per-event schema. Plain
// `-event` suffix is the common case; `-event-v{N}` covers versioned events.
func isEventAnchor(id string) bool {
	if strings.HasSuffix(id, "-event") {
		return true
	}
	return eventAnchorSuffixRe.MatchString(id)
}

// firstTableBeforeNextHeading returns the first <table> sibling appearing
// before the next <h1>/<h2>/<h3>/<h4> heading. Zero-length selection if none.
func firstTableBeforeNextHeading(h *goquery.Selection) *goquery.Selection {
	empty := &goquery.Selection{}
	for sib := h.Next(); sib.Length() > 0; sib = sib.Next() {
		switch goquery.NodeName(sib) {
		case "h1", "h2", "h3", "h4":
			return empty
		case "table":
			return sib
		}
	}
	return empty
}

// inlineRefHrefRe pulls `X-condition` out of a per-type section's condition-row href.
var inlineRefHrefRe = regexp.MustCompile(`/docs/eventsub/eventsub-reference/#([a-z0-9][a-z0-9-]*-condition)`)

// eventHrefRe pulls the event anchor out of a Notification Payload section's
// `event` row (matches any anchor, not just those ending in `-event`).
var eventHrefRe = regexp.MustCompile(`/docs/eventsub/eventsub-reference/?#([a-z0-9][a-z0-9-]*)`)

// parseSubscriptionTypes walks the master table and resolves each row's
// condition/event anchors. For rows whose per-type section links to the
// reference page, the link is authoritative. For rows whose per-type section
// inlines the condition schema in its request-body table, a synthetic entry
// is created in ref.Conditions keyed by the master anchor.
func parseSubscriptionTypes(doc *goquery.Document, ref *EventSubReference, log *slog.Logger) ([]EventSubSubscriptionType, error) {
	h1 := doc.Find("h1#subscription-types").First()
	if h1.Length() == 0 {
		return nil, fmt.Errorf("cannot find h1#subscription-types")
	}
	table := h1.NextAllFiltered("table").First()
	if table.Length() == 0 {
		return nil, fmt.Errorf("cannot find master subscription table")
	}

	type row struct {
		anchor, typeStr, version string
	}
	var rows []row

	table.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Children()
		if cells.Length() < 3 {
			return
		}
		href, _ := cells.Eq(0).Find("a").First().Attr("href")
		anchor := strings.TrimPrefix(href, "#")
		typeStr := strings.TrimSpace(cells.Eq(1).Text())
		version := strings.TrimSpace(cells.Eq(2).Text())
		if anchor == "" || typeStr == "" || version == "" {
			return
		}
		rows = append(rows, row{anchor: anchor, typeStr: typeStr, version: version})
	})

	if len(rows) == 0 {
		return nil, fmt.Errorf("master table parsed zero rows")
	}

	masterSet := make(map[string]bool, len(rows))
	for _, r := range rows {
		masterSet[r.anchor] = true
	}

	var out []EventSubSubscriptionType
	for _, r := range rows {
		sub := EventSubSubscriptionType{
			MasterAnchor: r.anchor,
			Type:         r.typeStr,
			Version:      r.version,
		}
		sub.ConditionAnchor, sub.EventAnchor = resolvePerSectionAnchors(doc, sub, ref, masterSet, log)
		out = append(out, sub)
	}
	return out, nil
}

// resolvePerSectionAnchors inspects the `<h3 id="{masterAnchor}">` section and
// returns the (conditionAnchor, eventAnchor). Strategy:
//
//  1. If the section's request-body table has a `condition` row with an
//     `eventsub-reference/#X-condition` link, use X-condition.
//  2. Otherwise, parse the request-body table itself: the rows following the
//     `condition` row (those with leading whitespace indentation) form the
//     inline condition schema. Key the synthetic schema by masterAnchor.
//  3. Event anchor: derive `{condition}-event` from the condition key and
//     verify it exists on the reference page; empty string if it doesn't.
func resolvePerSectionAnchors(doc *goquery.Document, sub EventSubSubscriptionType, ref *EventSubReference, masterSet map[string]bool, log *slog.Logger) (string, string) {
	section := doc.Find("h3#" + sub.MasterAnchor)
	if section.Length() == 0 {
		log.Warn("eventsub: no per-type section", "type", sub.Type, "version", sub.Version, "anchor", sub.MasterAnchor)
		return "", ""
	}

	sectionNodes := collectSectionNodes(section, masterSet)
	sectionHTML := serializeNodes(sectionNodes)

	condAnchor := ""
	if m := inlineRefHrefRe.FindStringSubmatch(sectionHTML); m != nil {
		condAnchor = m[1]
	} else {
		// Fallback: parse the inline condition schema directly.
		condAnchor = extractInlineConditionFromNodes(sectionNodes, sub, ref, log)
	}

	if condAnchor == "" {
		log.Warn("eventsub: no condition resolved", "type", sub.Type, "version", sub.Version)
		return "", ""
	}

	// Event anchor resolution, in order of preference:
	//   0. Manual override — for subscription types whose reference-page anchor
	//      lacks the `-event` suffix that isEventAnchor recognizes. The schema
	//      was routed into NamedSchemas by parseReferenceSchemas; promote it
	//      into Events so emitSchemaStructs materializes a typed event struct.
	//   1. The Notification Payload table in the same per-type section has an
	//      `event` row whose href points at a reference-page anchor.
	//   2. Swap "-condition" suffix for "-event" on the condition anchor —
	//      works for anchors shaped like "channel-follow-condition" → "channel-follow-event".
	//   3. Inline-parse a Notification Payload table whose `event` row has
	//      child rows (rare but exists for some subscription types).
	var eventAnchor string
	if override, ok := manualEventAnchorOverrides[[2]string{sub.Type, sub.Version}]; ok {
		if schema, exists := ref.NamedSchemas[override]; exists {
			ref.Events[override] = schema
			delete(ref.NamedSchemas, override)
		}
		if _, exists := ref.Events[override]; exists {
			eventAnchor = override
		}
	}
	if eventAnchor == "" {
		eventAnchor = findEventAnchorInSection(sectionHTML, ref)
	}
	if eventAnchor == "" {
		base := strings.TrimSuffix(condAnchor, "-condition")
		if _, ok := ref.Events[base+"-event"]; ok {
			eventAnchor = base + "-event"
		}
	}
	if eventAnchor == "" {
		eventAnchor = extractInlineEventFromNodes(sectionNodes, sub, ref, log)
	}
	if eventAnchor == "" {
		log.Warn("eventsub: no event anchor", "type", sub.Type, "version", sub.Version)
	}
	return condAnchor, eventAnchor
}

// manualEventAnchorOverrides maps (type, version) → reference-page anchor for
// subscription types whose reference-page section lacks the `-event` suffix
// that `isEventAnchor` detects. Without an override, `parseReferenceSchemas`
// files the schema under NamedSchemas, `findEventAnchorInSection` can't find
// it, and the dispatch falls through to UnknownEvent.
//
// Data-driven: only add entries confirmed by inspecting the reference page —
// the referenced anchor must exist and describe the full event payload.
var manualEventAnchorOverrides = map[[2]string]string{
	{"channel.shield_mode.begin", "1"}:  "shield-mode",
	{"channel.shield_mode.end", "1"}:    "shield-mode",
	{"channel.shoutout.create", "1"}:    "shoutout-create",
	{"channel.shoutout.receive", "1"}:   "shoutout-received",
}

// findEventAnchorInSection extracts the reference-page anchor the section's
// Notification Payload table points at for its `event` row.
func findEventAnchorInSection(sectionHTML string, ref *EventSubReference) string {
	// Narrow the search to the region starting at "Notification Payload" or
	// "Notification Payload Object" (Twitch uses both).
	idx := strings.Index(sectionHTML, "Notification Payload")
	if idx < 0 {
		return ""
	}
	region := sectionHTML[idx:]
	// Match the first eventsub-reference href after that marker and confirm
	// the referenced anchor exists in our parsed schemas.
	for _, m := range eventHrefRe.FindAllStringSubmatch(region, -1) {
		anchor := m[1]
		if anchor == "subscription" {
			continue // the `subscription` row precedes the `event` row
		}
		if _, ok := ref.Events[anchor]; ok {
			return anchor
		}
	}
	return ""
}

// extractInlineEventFromNodes searches the section's Notification Payload
// table for an inline `event` row with child fields and synthesizes an event
// schema keyed by masterAnchor+"-event".
func extractInlineEventFromNodes(nodes []*goquery.Selection, sub EventSubSubscriptionType, ref *EventSubReference, log *slog.Logger) string {
	for _, n := range nodes {
		var chosen string
		n.Find("table").EachWithBreak(func(_ int, table *goquery.Selection) bool {
			fields, err := ParseTable(table)
			if err != nil {
				if !errors.Is(err, ErrNotSchemaTable) {
					log.Warn("eventsub: probe ParseTable error", "type", sub.Type, "err", err)
				}
				return true
			}
			for i := range fields {
				if fields[i].Name == "event" && len(fields[i].Children) > 0 {
					setRequiredDefault(fields[i].Children, true, nil)
					key := inlineAnchorKey(sub, "-event")
					ref.Events[key] = EventSubSchema{AnchorID: key, Fields: fields[i].Children}
					log.Info("eventsub: inline event captured", "type", sub.Type, "key", key, "fields", len(fields[i].Children))
					chosen = key
					return false
				}
			}
			return true
		})
		if chosen != "" {
			return chosen
		}
	}
	return ""
}

// collectSectionNodes returns the h3 itself plus every following sibling up to
// (but not including) the next h3 whose id is another master anchor. Internal
// sub-headings within the section (authorization, request-body, etc.) stay
// inside the collected range.
func collectSectionNodes(h3 *goquery.Selection, masterSet map[string]bool) []*goquery.Selection {
	out := []*goquery.Selection{h3}
	for sib := h3.Next(); sib.Length() > 0; sib = sib.Next() {
		if goquery.NodeName(sib) == "h3" {
			if id, _ := sib.Attr("id"); masterSet[id] {
				break
			}
		}
		out = append(out, sib)
	}
	return out
}

func serializeNodes(nodes []*goquery.Selection) string {
	var sb strings.Builder
	for _, n := range nodes {
		if s, err := goquery.OuterHtml(n); err == nil {
			sb.WriteString(s)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// extractInlineConditionFromNodes looks across the section's `<table>` elements
// for a request-body table whose `condition` row has indented child rows that
// describe the schema inline. When found, a synthesized EventSubSchema keyed by
// masterAnchor+"-condition" is stored in ref.Conditions and that key is returned.
func extractInlineConditionFromNodes(nodes []*goquery.Selection, sub EventSubSubscriptionType, ref *EventSubReference, log *slog.Logger) string {
	for _, n := range nodes {
		var chosen string
		n.Find("table").EachWithBreak(func(_ int, table *goquery.Selection) bool {
			fields, err := ParseTable(table)
			if err != nil {
				if !errors.Is(err, ErrNotSchemaTable) {
					log.Warn("eventsub: probe ParseTable error", "type", sub.Type, "err", err)
				}
				return true
			}
			var condField *FieldSchema
			for i := range fields {
				if fields[i].Name == "condition" {
					condField = &fields[i]
					break
				}
			}
			if condField == nil || len(condField.Children) == 0 {
				return true
			}
			setRequiredDefault(condField.Children, true, nil)
			key := inlineAnchorKey(sub, "-condition")
			ref.Conditions[key] = EventSubSchema{
				AnchorID: key,
				Fields:   condField.Children,
			}
			chosen = key
			log.Info("eventsub: inline condition captured", "type", sub.Type, "version", sub.Version, "key", key, "fields", len(condField.Children))
			return false
		})
		if chosen != "" {
			return chosen
		}
		if goquery.NodeName(n) == "table" {
			// Same logic for top-level <table> nodes (rare).
			fields, err := ParseTable(n)
			if err != nil {
				continue
			}
			for i := range fields {
				if fields[i].Name == "condition" && len(fields[i].Children) > 0 {
					setRequiredDefault(fields[i].Children, true, nil)
					key := inlineAnchorKey(sub, "-condition")
					ref.Conditions[key] = EventSubSchema{
						AnchorID: key,
						Fields:   fields[i].Children,
					}
					log.Info("eventsub: inline condition captured", "type", sub.Type, "version", sub.Version, "key", key, "fields", len(fields[i].Children))
					return key
				}
			}
		}
	}
	return ""
}

// inlineAnchorKey produces a clean kebab-case key from the subscription type
// string, suitable for PascalCase naming. Versions > 1 are appended so distinct
// versions of the same type get distinct anchor keys.
var typeAnchorReplacer = strings.NewReplacer(".", "-", "_", "-")

func inlineAnchorKey(sub EventSubSubscriptionType, suffix string) string {
	base := typeAnchorReplacer.Replace(sub.Type)
	if sub.Version != "" && sub.Version != "1" {
		base += "-v" + sub.Version
	}
	return base + suffix
}
