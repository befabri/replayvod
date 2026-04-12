package main

import (
	"log/slog"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Normalize fixes known bugs in the Twitch reference HTML so that later phases
// can parse it without special cases. Each fix is ported verbatim from
// .reference/twitch-api-swagger/scripts/utils/normalizeReferenceHtml.ts.
// If a fix's target selector or substring no longer matches, a warning is logged
// (Twitch may have fixed the doc — audit the fix and remove it if obsolete).
func Normalize(doc *goquery.Document, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}

	// Wrong quotes and JS-style comments inside JSON example.
	// https://dev.twitch.tv/docs/api/reference/#modify-channel-information
	replaceCodeHTML(doc, log, "modify-channel-information", []rep{
		{search: "\u201c", value: `"`, all: true},
		{search: "\u201d", value: `"`, all: true},
		{search: "// adds this label", value: ""},
		{search: "// removes this label", value: ""},
	})

	// Wrong quotes in JSON example.
	// https://dev.twitch.tv/docs/api/reference/#get-shared-chat-session
	replaceCodeHTML(doc, log, "get-shared-chat-session", []rep{
		{search: "\u201c", value: `"`, all: true},
		{search: "\u201d", value: `"`, all: true},
	})

	// Missing trailing comma after "cheer 1" description.
	// https://dev.twitch.tv/docs/api/reference/#get-channel-chat-badges
	replaceCodeHTML(doc, log, "get-channel-chat-badges", []rep{
		{
			search: `<span class="s2">"description"</span><span class="p">:</span><span class="w"> </span><span class="s2">"cheer 1"</span>`,
			value:  `<span class="s2">"description"</span><span class="p">:</span><span class="w"> </span><span class="s2">"cheer 1"</span><span class="p">,</span>`,
		},
	})

	// Missing HTTP method prefix in URL.
	// https://dev.twitch.tv/docs/api/reference#get-stream-key
	replaceDocsHTML(doc, log, "get-stream-key", []rep{
		{
			search: "https://api.twitch.tv/helix/streams/key",
			value:  "GET https://api.twitch.tv/helix/streams/key",
		},
	})

	// Wrong response body indentation + duplicate wrapper row.
	// https://dev.twitch.tv/docs/api/reference/#get-content-classification-labels
	replaceDocsHTML(doc, log, "get-content-classification-labels", []rep{
		{
			search: "<tr>\n      <td>&nbsp; &nbsp;content_classification_labels</td>\n      <td>Label[]</td>\n      <td>The list of CCLs available.</td>\n    </tr>",
			value:  "",
		},
		{search: "<td>&nbsp; &nbsp; &nbsp; id</td>", value: "<td>&nbsp;&nbsp;&nbsp;id</td>"},
		{search: "<td>&nbsp; &nbsp; &nbsp; description</td>", value: "<td>&nbsp;&nbsp;&nbsp;description</td>"},
		{search: "<td>&nbsp; &nbsp; &nbsp; name</td>", value: "<td>&nbsp;&nbsp;&nbsp;name</td>"},
	})

	// Inject missing Response Codes tables.
	// https://dev.twitch.tv/docs/api/reference/#get-content-classification-labels
	// https://dev.twitch.tv/docs/api/reference/#get-moderated-channels
	injectResponseCodesTable(doc, log, "get-content-classification-labels", "Successfully retrieved the list of CCLs available.")
	injectResponseCodesTable(doc, log, "get-moderated-channels", "Successfully retrieved the list of moderated channels.")

	// Guest star endpoints missing SUCCESS response codes.
	// https://github.com/DmitryScaletta/twitch-api-swagger/issues/11
	for _, id := range []string{
		"get-channel-guest-star-settings",
		"get-guest-star-session",
		"create-guest-star-session",
		"end-guest-star-session",
		"get-guest-star-invites",
	} {
		prependLastTableRow(doc, log, id, "<tr><td>200 OK</td><td></td></tr>")
	}
	for _, id := range []string{
		"send-guest-star-invite", // NOT TESTED upstream
		"delete-guest-star-invite",
	} {
		prependLastTableRow(doc, log, id, "<tr><td>204 No Content</td><td></td></tr>")
	}

	// Redundant trailing comma on "{}," in example.
	// https://dev.twitch.tv/docs/api/reference/#get-conduit-shards
	replaceCodeHTML(doc, log, "get-conduit-shards", []rep{
		{search: `<span class="p">{},</span>`, value: `<span class="p">{}</span>`},
	})

	// Missing comma in example.
	// https://dev.twitch.tv/docs/api/reference/#update-conduit-shards
	replaceCodeHTML(doc, log, "update-conduit-shards", []rep{
		{
			search: `"https://this-is-a-callback-3.com"`,
			value:  `"https://this-is-a-callback-3.com",`,
		},
	})

	// Misplaced quote in request body.
	// https://dev.twitch.tv/docs/api/reference/#create-eventsub-subscription
	replaceCodeHTML(doc, log, "create-eventsub-subscription", []rep{
		{search: `"` + "\n    " + `type": "user.update"`, value: "\n    " + `"type": "user.update"`},
	})

	// Wrong <cost> tag should be <code>cost</code>.
	// https://dev.twitch.tv/docs/api/reference/#update-extension-bits-product
	replaceDocsHTML(doc, log, "update-extension-bits-product", []rep{
		{search: `&lt;cost&gt;cost&lt;/cost&gt;`, value: `<code>cost</code>`},
	})

	// Wrong padding for `broadcast`.
	// https://dev.twitch.tv/docs/api/reference/#get-extension-transactions
	replaceDocsHTML(doc, log, "get-extension-transactions", []rep{
		{
			search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp; broadcast</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;broadcast</td>`,
		},
	})

	// Wrong padding after `guests` + wrong type for guests.
	// https://dev.twitch.tv/docs/api/reference/#get-guest-star-session
	// https://dev.twitch.tv/docs/api/reference/#create-guest-star-session
	// https://dev.twitch.tv/docs/api/reference/#end-guest-star-session
	for _, id := range []string{
		"get-guest-star-session",
		"create-guest-star-session",
		"end-guest-star-session",
	} {
		fixGuestsPadding(doc, log, id)
	}

	// Wrong padding for `broadcaster_name`.
	// https://dev.twitch.tv/docs/api/reference/#get-unban-requests
	replaceDocsHTML(doc, log, "get-unban-requests", []rep{
		{
			search: `<td>&nbsp; &nbsp; &nbsp;&nbsp;&nbsp;broadcaster_name</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;broadcaster_name</td>`,
		},
	})

	// Wrong padding for `video_id`, `markers`, and their children.
	// https://dev.twitch.tv/docs/api/reference/#get-stream-markers
	replaceDocsHTML(doc, log, "get-stream-markers", []rep{
		{
			search: `<td>&nbsp;&nbsp;&nbsp;video_id</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;video_id</td>`,
		},
		{
			search: `<td>&nbsp;&nbsp;&nbsp;markers</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;markers</td>`,
		},
		{
			search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;id</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;id</td>`,
		},
		{
			search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;created_at</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;created_at</td>`,
		},
		{
			search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;description</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;description</td>`,
		},
		{
			search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;position_seconds</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;position_seconds</td>`,
		},
		{
			search: `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;url</td>`,
			value:  `<td>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;url</td>`,
		},
	})

	// Replace Array → String[].
	// https://dev.twitch.tv/docs/api/reference#add-suspicious-status-to-chat-user
	// https://dev.twitch.tv/docs/api/reference#remove-suspicious-status-from-chat-user
	for _, id := range []string{
		"add-suspicious-status-to-chat-user",
		"remove-suspicious-status-from-chat-user",
	} {
		replaceDocsHTML(doc, log, id, []rep{
			{search: `<td>Array</td>`, value: `<td>String[]</td>`},
		})
	}

	// Inject missing pagination rows.
	for _, id := range []string{
		"get-custom-reward-redemption",
		"search-categories",
		"search-channels",
		"get-user-block-list",
	} {
		injectPagination(doc, log, id)
	}

	// Wrong type for ad-schedule timestamps (String → Int64) + description tweak.
	// https://dev.twitch.tv/docs/api/reference#get-ad-schedule
	// https://dev.twitch.tv/docs/api/reference#snooze-next-ad
	const zero = `<code class="highlighter-rouge">0</code>`
	for _, id := range []string{"get-ad-schedule", "snooze-next-ad"} {
		replaceDocsHTML(doc, log, id, []rep{
			{
				search: `The UTC timestamp when the broadcaster will gain an additional snooze, in RFC3339 format.`,
				value:  `The UTC timestamp when the broadcaster will gain an additional snooze, in RFC3339 format. Can be ` + zero + `.`,
			},
		})
	}
	replaceDocsHTML(doc, log, "get-ad-schedule", []rep{
		{search: `Empty if the channel`, value: zero + ` if the channel`},
		{search: "snooze_refresh_at</td>\n      <td>String</td>", value: "snooze_refresh_at</td>\n      <td>Int64</td>"},
		{search: "next_ad_at</td>\n      <td>String</td>", value: "next_ad_at</td>\n      <td>Int64</td>"},
		{search: "last_ad_at</td>\n      <td>String</td>", value: "last_ad_at</td>\n      <td>Int64</td>"},
	})
	replaceDocsHTML(doc, log, "snooze-next-ad", []rep{
		{search: "snooze_refresh_at</td>\n      <td>String</td>", value: "snooze_refresh_at</td>\n      <td>Int64</td>"},
		{search: "next_ad_at</td>\n      <td>String</td>", value: "next_ad_at</td>\n      <td>Int64</td>"},
		{
			search: "The UTC timestamp of the broadcaster\u2019s next scheduled ad, in RFC3339 format.",
			value:  "The UTC timestamp of the broadcaster\u2019s next scheduled ad, in RFC3339 format. " + zero + " if the channel has no ad scheduled or is not live.",
		},
	})
}

// rep is one find/replace operation against an element's innerHTML.
type rep struct {
	search string
	value  string
	all    bool // true → replaceAll, false → replace first occurrence
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

func replaceDocsHTML(doc *goquery.Document, log *slog.Logger, id string, reps []rep) {
	el := docsEl(doc, id)
	if el == nil {
		log.Warn("normalize: docs element not found", "endpoint", id)
		return
	}
	applyReps(log, id, el, reps)
}

func replaceCodeHTML(doc *goquery.Document, log *slog.Logger, id string, reps []rep) {
	el := codeEl(doc, id)
	if el == nil {
		log.Warn("normalize: code element not found", "endpoint", id)
		return
	}
	applyReps(log, id, el, reps)
}

func applyReps(log *slog.Logger, id string, sel *goquery.Selection, reps []rep) {
	html, err := sel.Html()
	if err != nil {
		log.Warn("normalize: read html", "endpoint", id, "err", err)
		return
	}
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
			log.Warn("normalize: replace not applied", "endpoint", id, "search", truncateForLog(r.search))
		}
	}
	sel.SetHtml(html)
}

// decodeEntitiesForMatch converts a few common HTML entities to their literal
// characters so that search strings ported verbatim from the TS normalizer
// (where jsdom preserves entities) match goquery's decoded innerHTML.
var entityReplacer = strings.NewReplacer(
	"&nbsp;", "\u00a0",
)

func decodeEntitiesForMatch(s string) string {
	return entityReplacer.Replace(s)
}

func truncateForLog(s string) string {
	const max = 80
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// injectResponseCodesTable appends a default Response Codes section.
func injectResponseCodesTable(doc *goquery.Document, log *slog.Logger, id, okDesc string) {
	el := docsEl(doc, id)
	if el == nil {
		log.Warn("normalize: docs element not found", "endpoint", id)
		return
	}
	table := `<table>
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
	html, err := el.Html()
	if err != nil {
		log.Warn("normalize: read html", "endpoint", id, "err", err)
		return
	}
	el.SetHtml(html + "<h3>Response Codes</h3>" + table)
}

// prependLastTableRow inserts `row` at the start of the last `<table>`'s tbody.
func prependLastTableRow(doc *goquery.Document, log *slog.Logger, id, row string) {
	el := docsEl(doc, id)
	if el == nil {
		log.Warn("normalize: docs element not found", "endpoint", id)
		return
	}
	tables := el.Find("table")
	if tables.Length() == 0 {
		log.Warn("normalize: no tables", "endpoint", id)
		return
	}
	last := tables.Eq(tables.Length() - 1)
	applyReps(log, id, last, []rep{
		{search: "<tbody>", value: "<tbody>" + row},
	})
}

// fixGuestsPadding fixes the guest-star `guests` field type and adds 3 nbsp of padding
// to all rows after it in the second table.
func fixGuestsPadding(doc *goquery.Document, log *slog.Logger, id string) {
	el := docsEl(doc, id)
	if el == nil {
		log.Warn("normalize: docs element not found", "endpoint", id)
		return
	}
	tables := el.Find("table")
	if tables.Length() < 2 {
		log.Warn("normalize: fewer than 2 tables", "endpoint", id)
		return
	}
	second := tables.Eq(1)
	const guestsField = "<td>Guest</td>"
	addPadding := false
	second.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		if addPadding {
			applyReps(log, id, tr, []rep{
				{search: "<td>", value: "<td>&nbsp;&nbsp;&nbsp"},
			})
			return
		}
		html, err := tr.Html()
		if err != nil {
			return
		}
		if strings.Contains(html, guestsField) {
			applyReps(log, id, tr, []rep{
				{search: guestsField, value: "<td>Object[]</td>"},
			})
			addPadding = true
		}
	})
	if !addPadding {
		log.Warn("normalize: guests field not found", "endpoint", id)
	}
}

// injectPagination appends pagination fields to the end of the second table's tbody.
func injectPagination(doc *goquery.Document, log *slog.Logger, id string) {
	el := docsEl(doc, id)
	if el == nil {
		log.Warn("normalize: docs element not found", "endpoint", id)
		return
	}
	tables := el.Find("table")
	if tables.Length() < 2 {
		log.Warn("normalize: fewer than 2 tables", "endpoint", id)
		return
	}
	paginationRows := "\n      <tr>\n" +
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
	applyReps(log, id, tables.Eq(1), []rep{
		{search: "</tbody>", value: paginationRows + "</tbody>"},
	})
}
