package main

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// fixScope picks whether a fix mutates `.left-docs` (the documentation pane)
// or `.right-code` (the example pane) for its endpoint anchor.
type fixScope int

const (
	scopeDocs fixScope = iota
	scopeCode
)

// fixDescriptor is one entry in the normalize registry. `Apply` receives the
// resolved wrapper element at Endpoint and mutates it in place; Normalize
// handles a nil wrapper (missing anchor) before invoking Apply.
type fixDescriptor struct {
	Name     string
	Endpoint string
	Scope    fixScope
	Apply    func(el *goquery.Selection, log *slog.Logger) error
}

// rep is one find/replace operation against an element's innerHTML.
type rep struct {
	search string
	value  string
	all    bool // true → replaceAll, false → replace first occurrence
}

// Normalize fixes known bugs in the Twitch reference HTML so that later phases
// can parse it without special cases. Each entry in normalizeFixes is ported
// verbatim from .reference/twitch-api-swagger/scripts/utils/normalizeReferenceHtml.ts.
//
// A fix that can't locate its target element or that leaves its input
// untouched is logged; the per-fix and smoke tests in normalize_test.go treat
// the same conditions as failures so matcher bugs surface at CI time instead
// of via log scraping.
func Normalize(doc *goquery.Document, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	for _, r := range RunFixes(doc, log) {
		if r.Err != nil {
			log.Warn("normalize: fix failed", "fix", r.Name, "endpoint", r.Endpoint, "err", r.Err)
		}
	}
}

// FixResult captures the outcome of a single fix run — used by tests to treat
// "fix didn't apply" as a failure instead of a log line.
type FixResult struct {
	Name     string
	Endpoint string
	Err      error
}

// RunFixes applies every fix in registry order and returns per-fix outcomes.
// Earlier fixes' mutations feed into later fixes, matching production.
func RunFixes(doc *goquery.Document, log *slog.Logger) []FixResult {
	out := make([]FixResult, 0, len(normalizeFixes))
	for _, fix := range normalizeFixes {
		res := FixResult{Name: fix.Name, Endpoint: fix.Endpoint}
		el := resolveScope(doc, fix.Scope, fix.Endpoint)
		if el == nil {
			res.Err = fmt.Errorf("element not found")
			out = append(out, res)
			continue
		}
		res.Err = fix.Apply(el, log)
		out = append(out, res)
	}
	return out
}

// resolveScope returns the `.left-docs` or `.right-code` wrapper for an
// endpoint anchor, or nil if the anchor doesn't exist.
func resolveScope(doc *goquery.Document, scope fixScope, endpoint string) *goquery.Selection {
	switch scope {
	case scopeDocs:
		return docsEl(doc, endpoint)
	case scopeCode:
		return codeEl(doc, endpoint)
	}
	return nil
}

// docsEl returns the `.left-docs` ancestor of the given endpoint anchor, or nil.
func docsEl(doc *goquery.Document, id string) *goquery.Selection {
	anchor := doc.Find("#" + id)
	if anchor.Length() == 0 {
		return nil
	}
	el := anchor.Closest(".left-docs")
	if el.Length() == 0 {
		return nil
	}
	return el
}

// codeEl returns the `.right-code` sibling of the given endpoint, or nil.
func codeEl(doc *goquery.Document, id string) *goquery.Selection {
	anchor := doc.Find("#" + id)
	if anchor.Length() == 0 {
		return nil
	}
	content := anchor.Closest(".doc-content")
	if content.Length() == 0 {
		return nil
	}
	el := content.Find(".right-code").First()
	if el.Length() == 0 {
		return nil
	}
	return el
}

// --- Apply constructors ---

// replaceFix builds a find/replace fix against the wrapper's innerHTML. Returns
// an error if any search string produces no change — the smoke test treats
// that as a failure so a silent matcher regression surfaces immediately.
func replaceFix(endpoint, name string, scope fixScope, reps []rep) fixDescriptor {
	return fixDescriptor{
		Name:     endpoint + "-" + name,
		Endpoint: endpoint,
		Scope:    scope,
		Apply: func(el *goquery.Selection, _ *slog.Logger) error {
			return applyReps(el, reps)
		},
	}
}

// customFix wraps a caller-provided Apply closure for inject/mutate shapes
// that don't fit a set of reps.
func customFix(endpoint, name string, scope fixScope, apply func(*goquery.Selection, *slog.Logger) error) fixDescriptor {
	return fixDescriptor{
		Name:     endpoint + "-" + name,
		Endpoint: endpoint,
		Scope:    scope,
		Apply:    apply,
	}
}

// applyReps runs every rep against the wrapper's innerHTML, returning an
// error that lists the reps which produced no change.
func applyReps(el *goquery.Selection, reps []rep) error {
	html, err := el.Html()
	if err != nil {
		return fmt.Errorf("read html: %w", err)
	}
	var misses []string
	for _, r := range reps {
		search := decodeEntitiesForMatch(r.search)
		value := decodeEntitiesForMatch(r.value)
		before := html
		if r.all {
			html = strings.ReplaceAll(html, search, value)
		} else {
			html = strings.Replace(html, search, value, 1)
		}
		if html == before {
			misses = append(misses, truncateForLog(r.search))
		}
	}
	el.SetHtml(html)
	if len(misses) > 0 {
		return fmt.Errorf("replace not applied: %v", misses)
	}
	return nil
}

// decodeEntitiesForMatch rewrites a search/value string (ported from the TS
// normalizer or hand-written against the raw HTML source) into the form that
// goquery's `.Html()` round-trips to. It must match what goquery PRODUCES on
// serialization, not what the HTML source contains — misses here surface as
// silently no-op normalize fixes. Known mappings:
//
//   - `&nbsp;` anywhere → `\u00a0` (goquery decodes the entity and stores it
//     as the literal character).
//   - `"` in TEXT content → `&#34;`. `'` → `&#39;`. Goquery re-encodes these
//     on serialization; attribute values (e.g. `class="x"`) are preserved
//     with literal quotes, so we deliberately skip them via tag-splitting.
//
// Extend here only after confirming via `sel.Html()` what goquery actually
// emits — the encoding is context-sensitive, so a flat `strings.NewReplacer`
// is the wrong tool.
func decodeEntitiesForMatch(s string) string {
	s = strings.ReplaceAll(s, "&nbsp;", "\u00a0")
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for _, idx := range htmlTagRe.FindAllStringIndex(s, -1) {
		b.WriteString(encodeTextContentQuotes(s[last:idx[0]]))
		b.WriteString(s[idx[0]:idx[1]])
		last = idx[1]
	}
	b.WriteString(encodeTextContentQuotes(s[last:]))
	return b.String()
}

// htmlTagRe matches an HTML tag (open or close, with attrs). The regions
// BETWEEN these matches are text content, where quote encoding differs from
// attribute values.
//
// Naive: the pattern breaks on a tag whose attribute value contains a literal
// `>` (e.g. `<a title="5>3">`). Twitch's docs don't emit that shape in any
// fix's search/value string, so we accept the limitation. If a future fix's
// string ever does, the fix won't apply and TestNormalize_AllFixesApply fires.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

var textQuoteReplacer = strings.NewReplacer(`"`, "&#34;", "'", "&#39;")

func encodeTextContentQuotes(s string) string { return textQuoteReplacer.Replace(s) }

func truncateForLog(s string) string {
	const max = 80
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// --- Custom fix apply functions ---

// responseCodesTable is the default-shape Response Codes section Twitch omits
// for the endpoints in injectResponseCodesTable's callers.
func responseCodesTable(okDesc string) string {
	return `<table>
      <thead>
        <tr>
          <th>Code</th>
          <th>Description</th>
        </tr>
      </thead>
      <tbody>
        <tr>
          <td>200 OK</td>
          <td>` + okDesc + `</td>
        </tr>
        <tr>
          <td>400 Bad Request</td>
          <td></td>
        </tr>
        <tr>
          <td>401 Unauthorized</td>
          <td></td>
        </tr>
        <tr>
          <td>500 Internal Server Error</td>
          <td></td>
        </tr>
      </tbody>
    </table>`
}

// applyInjectResponseCodes appends a default Response Codes section to the
// .left-docs wrapper for endpoints whose Twitch docs page omits one.
func applyInjectResponseCodes(okDesc string) func(*goquery.Selection, *slog.Logger) error {
	return func(el *goquery.Selection, _ *slog.Logger) error {
		html, err := el.Html()
		if err != nil {
			return fmt.Errorf("read html: %w", err)
		}
		el.SetHtml(html + "<h3>Response Codes</h3>" + responseCodesTable(okDesc))
		return nil
	}
}

// applyPrependLastTableRow inserts `row` at the start of the last `<table>`'s
// tbody within the wrapper (used to inject missing SUCCESS response codes).
func applyPrependLastTableRow(row string) func(*goquery.Selection, *slog.Logger) error {
	return func(el *goquery.Selection, _ *slog.Logger) error {
		tables := el.Find("table")
		if tables.Length() == 0 {
			return fmt.Errorf("no tables in element")
		}
		last := tables.Eq(tables.Length() - 1)
		return applyReps(last, []rep{{search: "<tbody>", value: "<tbody>" + row}})
	}
}

// applyFixGuestsPadding corrects the guest-star `guests` field type and adds
// three nbsp of padding to every row after it in the second table.
func applyFixGuestsPadding(el *goquery.Selection, _ *slog.Logger) error {
	tables := el.Find("table")
	if tables.Length() < 2 {
		return fmt.Errorf("fewer than 2 tables")
	}
	second := tables.Eq(1)
	const guestsField = "<td>Guest</td>"
	var (
		addPadding bool
		padErr     error
	)
	second.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		if addPadding {
			if err := applyReps(tr, []rep{{search: "<td>", value: "<td>&nbsp;&nbsp;&nbsp"}}); err != nil {
				padErr = err
			}
			return
		}
		html, err := tr.Html()
		if err != nil {
			return
		}
		if strings.Contains(html, guestsField) {
			if err := applyReps(tr, []rep{{search: guestsField, value: "<td>Object[]</td>"}}); err != nil {
				padErr = err
				return
			}
			addPadding = true
		}
	})
	if !addPadding {
		return fmt.Errorf("guests field not found")
	}
	return padErr
}

// paginationRows is the pagination schema Twitch omits from the endpoints in
// injectPagination's callers.
const paginationRows = "\n      <tr>\n" +
	"        <td>pagination</td>\n" +
	"        <td>Object</td>\n" +
	"        <td>\n" +
	"          Contains the information used to page through the list of results.\n" +
	"          The object is empty if there are no more pages left to page through.\n" +
	"          <a href=\"/docs/api/guide#pagination\">Read More</a>\n" +
	"        </td>\n" +
	"      </tr>\n" +
	"      <tr>\n" +
	"        <td>&nbsp;&nbsp;&nbsp;cursor</td>\n" +
	"        <td>String</td>\n" +
	"        <td>\n" +
	"          The cursor used to get the next page of results.\n" +
	"          Use the cursor to set the request\u2019s <em>after</em> query parameter.\n" +
	"        </td>\n" +
	"      </tr>\n    "

// applyInjectPagination appends pagination rows to the second table's tbody.
func applyInjectPagination(el *goquery.Selection, _ *slog.Logger) error {
	tables := el.Find("table")
	if tables.Length() < 2 {
		return fmt.Errorf("fewer than 2 tables")
	}
	return applyReps(tables.Eq(1), []rep{{search: "</tbody>", value: paginationRows + "</tbody>"}})
}

// --- Registry ---

const zero = `<code class="highlighter-rouge">0</code>`

var normalizeFixes = []fixDescriptor{
	// Curly quotes and JS-style comments in modify-channel-information example.
	// https://dev.twitch.tv/docs/api/reference/#modify-channel-information
	replaceFix("modify-channel-information", "quotes", scopeCode, []rep{
		{search: "\u201c", value: `"`, all: true},
		{search: "\u201d", value: `"`, all: true},
		{search: "// adds this label", value: ""},
		{search: "// removes this label", value: ""},
	}),

	// Curly quotes in get-shared-chat-session example.
	// https://dev.twitch.tv/docs/api/reference/#get-shared-chat-session
	replaceFix("get-shared-chat-session", "quotes", scopeCode, []rep{
		{search: "\u201c", value: `"`, all: true},
		{search: "\u201d", value: `"`, all: true},
	}),

	// Missing trailing comma after "cheer 1" description.
	// https://dev.twitch.tv/docs/api/reference/#get-channel-chat-badges
	replaceFix("get-channel-chat-badges", "comma", scopeCode, []rep{
		{
			search: `<span class="s2">"description"</span><span class="p">:</span><span class="w"> </span><span class="s2">"cheer 1"</span>`,
			value:  `<span class="s2">"description"</span><span class="p">:</span><span class="w"> </span><span class="s2">"cheer 1"</span><span class="p">,</span>`,
		},
	}),

	// Missing HTTP method prefix in URL.
	// https://dev.twitch.tv/docs/api/reference#get-stream-key
	replaceFix("get-stream-key", "method", scopeDocs, []rep{
		{
			search: "https://api.twitch.tv/helix/streams/key",
			value:  "GET https://api.twitch.tv/helix/streams/key",
		},
	}),

	// Wrong response body indentation + duplicate wrapper row.
	// https://dev.twitch.tv/docs/api/reference/#get-content-classification-labels
	replaceFix("get-content-classification-labels", "rows", scopeDocs, []rep{
		{
			search: "<tr>\n      <td>&nbsp; &nbsp;content_classification_labels</td>\n      <td>Label[]</td>\n      <td>The list of CCLs available.</td>\n    </tr>",
			value:  "",
		},
		{search: "<td>&nbsp; &nbsp; &nbsp; id</td>", value: "<td>&nbsp;&nbsp;&nbsp;id</td>"},
		{search: "<td>&nbsp; &nbsp; &nbsp; description</td>", value: "<td>&nbsp;&nbsp;&nbsp;description</td>"},
		{search: "<td>&nbsp; &nbsp; &nbsp; name</td>", value: "<td>&nbsp;&nbsp;&nbsp;name</td>"},
	}),

	// Missing Response Codes tables.
	// https://dev.twitch.tv/docs/api/reference/#get-content-classification-labels
	// https://dev.twitch.tv/docs/api/reference/#get-moderated-channels
	customFix("get-content-classification-labels", "response-codes", scopeDocs,
		applyInjectResponseCodes("Successfully retrieved the list of CCLs available.")),
	customFix("get-moderated-channels", "response-codes", scopeDocs,
		applyInjectResponseCodes("Successfully retrieved the list of moderated channels.")),

	// Guest-star endpoints missing SUCCESS response codes.
	// https://github.com/DmitryScaletta/twitch-api-swagger/issues/11
	customFix("get-channel-guest-star-settings", "status-200", scopeDocs,
		applyPrependLastTableRow("<tr><td>200 OK</td><td></td></tr>")),
	customFix("get-guest-star-session", "status-200", scopeDocs,
		applyPrependLastTableRow("<tr><td>200 OK</td><td></td></tr>")),
	customFix("create-guest-star-session", "status-200", scopeDocs,
		applyPrependLastTableRow("<tr><td>200 OK</td><td></td></tr>")),
	customFix("end-guest-star-session", "status-200", scopeDocs,
		applyPrependLastTableRow("<tr><td>200 OK</td><td></td></tr>")),
	customFix("get-guest-star-invites", "status-200", scopeDocs,
		applyPrependLastTableRow("<tr><td>200 OK</td><td></td></tr>")),
	customFix("send-guest-star-invite", "status-204", scopeDocs, // NOT TESTED upstream
		applyPrependLastTableRow("<tr><td>204 No Content</td><td></td></tr>")),
	customFix("delete-guest-star-invite", "status-204", scopeDocs,
		applyPrependLastTableRow("<tr><td>204 No Content</td><td></td></tr>")),

	// Redundant trailing comma on "{},".
	// https://dev.twitch.tv/docs/api/reference/#get-conduit-shards
	replaceFix("get-conduit-shards", "comma", scopeCode, []rep{
		{search: `<span class="p">{},</span>`, value: `<span class="p">{}</span>`},
	}),

	// Missing comma in example.
	// https://dev.twitch.tv/docs/api/reference/#update-conduit-shards
	replaceFix("update-conduit-shards", "comma", scopeCode, []rep{
		{
			search: `"https://this-is-a-callback-3.com"`,
			value:  `"https://this-is-a-callback-3.com",`,
		},
	}),

	// Misplaced quote in request body.
	// https://dev.twitch.tv/docs/api/reference/#create-eventsub-subscription
	replaceFix("create-eventsub-subscription", "quote", scopeCode, []rep{
		{search: `"` + "\n    " + `type": "user.update"`, value: "\n    " + `"type": "user.update"`},
	}),

	// Wrong <cost> tag should be <code>cost</code>.
	// https://dev.twitch.tv/docs/api/reference/#update-extension-bits-product
	replaceFix("update-extension-bits-product", "tag", scopeDocs, []rep{
		{search: `&lt;cost&gt;cost&lt;/cost&gt;`, value: `<code>cost</code>`},
	}),

	// Wrong padding for `broadcast`.
	// https://dev.twitch.tv/docs/api/reference/#get-extension-transactions
	replaceFix("get-extension-transactions", "padding", scopeDocs, []rep{
		{
			search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp; broadcast</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;broadcast</td>`,
		},
	}),

	// Wrong padding after `guests` + wrong type for guests.
	// https://dev.twitch.tv/docs/api/reference/#get-guest-star-session
	// https://dev.twitch.tv/docs/api/reference/#create-guest-star-session
	// https://dev.twitch.tv/docs/api/reference/#end-guest-star-session
	customFix("get-guest-star-session", "guests-padding", scopeDocs, applyFixGuestsPadding),
	customFix("create-guest-star-session", "guests-padding", scopeDocs, applyFixGuestsPadding),
	customFix("end-guest-star-session", "guests-padding", scopeDocs, applyFixGuestsPadding),

	// Wrong padding for `broadcaster_name`.
	// https://dev.twitch.tv/docs/api/reference/#get-unban-requests
	replaceFix("get-unban-requests", "padding", scopeDocs, []rep{
		{
			search: `<td>&nbsp; &nbsp; &nbsp;&nbsp;&nbsp;broadcaster_name</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;broadcaster_name</td>`,
		},
	}),

	// Wrong padding for `video_id`, `markers`, and their children.
	// https://dev.twitch.tv/docs/api/reference/#get-stream-markers
	replaceFix("get-stream-markers", "padding", scopeDocs, []rep{
		{search: `<td>&nbsp;&nbsp;&nbsp;video_id</td>`, value: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;video_id</td>`},
		{search: `<td>&nbsp;&nbsp;&nbsp;markers</td>`, value: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;markers</td>`},
		{search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;id</td>`, value: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;id</td>`},
		{search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;created_at</td>`, value: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;created_at</td>`},
		{search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;description</td>`, value: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;description</td>`},
		{search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;position_seconds</td>`, value: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;position_seconds</td>`},
		{search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;url</td>`, value: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;url</td>`},
	}),

	// Replace Array → String[].
	// https://dev.twitch.tv/docs/api/reference#add-suspicious-status-to-chat-user
	// https://dev.twitch.tv/docs/api/reference#remove-suspicious-status-from-chat-user
	replaceFix("add-suspicious-status-to-chat-user", "array", scopeDocs, []rep{
		{search: `<td>Array</td>`, value: `<td>String[]</td>`},
	}),
	replaceFix("remove-suspicious-status-from-chat-user", "array", scopeDocs, []rep{
		{search: `<td>Array</td>`, value: `<td>String[]</td>`},
	}),

	// Missing pagination rows.
	customFix("get-custom-reward-redemption", "pagination", scopeDocs, applyInjectPagination),
	customFix("search-categories", "pagination", scopeDocs, applyInjectPagination),
	customFix("search-channels", "pagination", scopeDocs, applyInjectPagination),
	customFix("get-user-block-list", "pagination", scopeDocs, applyInjectPagination),

	// Ad-schedule description tweak ("Can be 0").
	// https://dev.twitch.tv/docs/api/reference#get-ad-schedule
	// https://dev.twitch.tv/docs/api/reference#snooze-next-ad
	replaceFix("get-ad-schedule", "snooze-desc", scopeDocs, []rep{
		{
			search: `The UTC timestamp when the broadcaster will gain an additional snooze, in RFC3339 format.`,
			value:  `The UTC timestamp when the broadcaster will gain an additional snooze, in RFC3339 format. Can be ` + zero + `.`,
		},
	}),
	replaceFix("snooze-next-ad", "snooze-desc", scopeDocs, []rep{
		{
			search: `The UTC timestamp when the broadcaster will gain an additional snooze, in RFC3339 format.`,
			value:  `The UTC timestamp when the broadcaster will gain an additional snooze, in RFC3339 format. Can be ` + zero + `.`,
		},
	}),

	// String → Int64 for ad-schedule timestamps + "Empty" → 0 tweak.
	replaceFix("get-ad-schedule", "types", scopeDocs, []rep{
		{search: `Empty if the channel`, value: zero + ` if the channel`},
		{search: "snooze_refresh_at</td>\n      <td>String</td>", value: "snooze_refresh_at</td>\n      <td>Int64</td>"},
		{search: "next_ad_at</td>\n      <td>String</td>", value: "next_ad_at</td>\n      <td>Int64</td>"},
		{search: "last_ad_at</td>\n      <td>String</td>", value: "last_ad_at</td>\n      <td>Int64</td>"},
	}),
	replaceFix("snooze-next-ad", "types", scopeDocs, []rep{
		{search: "snooze_refresh_at</td>\n      <td>String</td>", value: "snooze_refresh_at</td>\n      <td>Int64</td>"},
		{search: "next_ad_at</td>\n      <td>String</td>", value: "next_ad_at</td>\n      <td>Int64</td>"},
		{
			search: "The UTC timestamp of the broadcaster\u2019s next scheduled ad, in RFC3339 format.",
			value:  "The UTC timestamp of the broadcaster\u2019s next scheduled ad, in RFC3339 format. " + zero + " if the channel has no ad scheduled or is not live.",
		},
	}),
}
