package main

import (
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ParseAll walks the reference document and returns endpoint definitions for
// every ID in `filter`. Unknown IDs are reported as warnings so the caller can
// notice stale filter entries after Twitch renames an endpoint.
func ParseAll(doc *goquery.Document, filter []string, log *slog.Logger) ([]EndpointDef, error) {
	if log == nil {
		log = slog.Default()
	}
	meta := parseMasterTable(doc)

	want := make(map[string]bool, len(filter))
	for _, id := range filter {
		want[id] = true
	}

	var out []EndpointDef
	seen := map[string]bool{}
	var perr error

	doc.Find("section.doc-content").EachWithBreak(func(_ int, section *goquery.Selection) bool {
		h2 := section.Find("h2").First()
		id, _ := h2.Attr("id")
		if id == "" || !want[id] {
			return true
		}
		seen[id] = true
		ep, err := parseEndpoint(id, section, meta[id], log)
		if err != nil {
			perr = fmt.Errorf("endpoint %q: %w", id, err)
			return false
		}
		out = append(out, ep)
		return true
	})
	if perr != nil {
		return nil, perr
	}

	for _, id := range filter {
		if !seen[id] {
			log.Warn("parser: endpoint in filter not found in docs", "endpoint", id)
		}
	}
	return out, nil
}

// parseMasterTable reads the `#twitch-api-reference` summary table into a map.
func parseMasterTable(doc *goquery.Document) map[string]EndpointMeta {
	out := map[string]EndpointMeta{}
	doc.Find("#twitch-api-reference + table tbody tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Children()
		if cells.Length() < 3 {
			return
		}
		tag := strings.TrimSpace(cells.Eq(0).Text())
		anchor := cells.Eq(1).Find("a").First()
		href, _ := anchor.Attr("href")
		id := strings.TrimPrefix(href, "#")
		name := strings.TrimSpace(anchor.Text())
		summary := strings.TrimSpace(cells.Eq(2).Text())
		if id == "" {
			return
		}
		out[id] = EndpointMeta{ID: id, Tag: tag, Summary: summary, Name: name}
	})
	return out
}

// scopeRegex matches `**scope:name**` (bold) and `` `scope:name` `` (inline code)
// anywhere in the authentication/authorization description paragraphs.
// After decoding, only tokens containing ':' are retained.
var scopeRegex = regexp.MustCompile(`\*\*([a-z:_\\]+)\*\*|` + "`" + `([a-z:_]+)` + "`")

// parseEndpoint walks a <section.doc-content> for one endpoint.
func parseEndpoint(id string, section *goquery.Selection, meta EndpointMeta, log *slog.Logger) (EndpointDef, error) {
	leftDocs := section.Find(".left-docs").First()
	if leftDocs.Length() == 0 {
		return EndpointDef{}, fmt.Errorf("no .left-docs section")
	}

	ep := EndpointDef{
		ID:      id,
		Name:    meta.Name,
		Tag:     meta.Tag,
		Summary: meta.Summary,
	}

	var descriptionLines []string
	var authLines []string
	currentSection := "description"

	var outerErr error
	leftDocs.Children().EachWithBreak(func(_ int, el *goquery.Selection) bool {
		tag := goquery.NodeName(el)
		class, _ := el.Attr("class")

		if tag == "h2" || class == "editor-link" {
			return true
		}
		if tag == "h3" {
			currentSection = strings.TrimSpace(el.Text())
			return true
		}

		lower := strings.ToLower(currentSection)
		elText := strings.TrimSpace(el.Text())
		elHTML, _ := el.Html()

		switch {
		case currentSection == "description":
			if elText != "" {
				descriptionLines = append(descriptionLines, elText)
			}

		case currentSection == "Authentication" || currentSection == "Authorization":
			if elHTML != "" {
				authLines = append(authLines, elHTML)
			}
			// Also collect scope tokens from <code> and <strong> descendants so
			// extractScopes can match them without an HTML-to-markdown step.
			el.Find("code, strong").Each(func(_ int, s *goquery.Selection) {
				authLines = append(authLines, "`"+strings.TrimSpace(s.Text())+"`")
			})

		case strings.Contains(lower, "url"):
			method, url := parseMethodURL(elText)
			if method != "" {
				ep.Method = method
			}
			if url != "" {
				ep.Path = strings.TrimPrefix(url, "https://api.twitch.tv/helix")
			}

		case strings.Contains(lower, "query parameter"):
			if tag == "table" {
				fields, err := ParseTable(el)
				if err != nil {
					outerErr = fmt.Errorf("query parameters: %w", err)
					return false
				}
				for _, f := range fields {
					if len(f.Children) > 0 {
						outerErr = fmt.Errorf("query parameter %q has children", f.Name)
						return false
					}
				}
				ep.QueryParams = fields
			}

		case strings.Contains(lower, "request body"):
			if tag == "table" {
				fields, err := ParseTable(el)
				if err != nil {
					outerErr = fmt.Errorf("request body: %w", err)
					return false
				}
				setRequiredDefault(fields, true, nil)
				ep.BodyFields = fields
			}

		case strings.Contains(lower, "response body"):
			if tag == "table" {
				fields, err := ParseTable(el)
				if err != nil {
					outerErr = fmt.Errorf("response body: %w", err)
					return false
				}
				setRequiredDefault(fields, true, func(f *FieldSchema) bool {
					return f.Name == "pagination" || f.Name == "cursor"
				})
				ep.Response = fields
			}

		case lower == "response codes":
			if tag == "table" {
				codes, err := parseStatusCodes(el)
				if err != nil {
					outerErr = fmt.Errorf("response codes: %w", err)
					return false
				}
				ep.StatusCodes = codes
			}
		}
		return true
	})
	if outerErr != nil {
		return EndpointDef{}, outerErr
	}

	ep.Description = strings.TrimSpace(strings.Join(descriptionLines, "\n\n"))
	ep.Scopes = extractScopes(authLines)
	ep.AuthType = detectAuthType(authLines)
	ep.Deprecated = isDeprecated(ep.Description, ep.Summary)

	if ep.Method == "" || ep.Path == "" {
		log.Warn("parser: endpoint missing method/url", "endpoint", id, "method", ep.Method, "path", ep.Path)
	}
	return ep, nil
}

// parseMethodURL parses "GET https://api.twitch.tv/helix/users" → ("GET", "https://...").
func parseMethodURL(s string) (string, string) {
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// setRequiredDefault walks the tree and fills in Required where it's nil.
// When `force` returns true for a field, Required becomes false regardless.
func setRequiredDefault(fields []FieldSchema, defaultRequired bool, force func(*FieldSchema) bool) {
	for i := range fields {
		f := &fields[i]
		if f.Required == nil {
			v := defaultRequired
			if force != nil && force(f) {
				v = false
			}
			f.Required = &v
		}
		setRequiredDefault(f.Children, defaultRequired, force)
	}
}

// parseStatusCodes reads the Response Codes table.
func parseStatusCodes(table *goquery.Selection) ([]StatusCode, error) {
	var out []StatusCode
	table.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Children()
		if cells.Length() < 2 {
			return
		}
		code := strings.TrimSpace(cells.Eq(0).Text())
		desc := strings.TrimSpace(collapseWhitespace(cells.Eq(1).Text()))
		if len(code) < 3 {
			return
		}
		n, err := strconv.Atoi(code[:3])
		if err != nil {
			return
		}
		out = append(out, StatusCode{Code: n, Description: desc})
	})
	return out, nil
}

// extractScopes finds `**scope:name**` and backtick-wrapped scopes in auth HTML.
func extractScopes(lines []string) []string {
	var out []string
	for _, line := range lines {
		for _, m := range scopeRegex.FindAllStringSubmatch(line, -1) {
			scope := m[1]
			if scope == "" {
				scope = m[2]
			}
			if !strings.Contains(scope, ":") {
				continue
			}
			scope = strings.ReplaceAll(scope, `\`, "")
			if slices.Contains(out, scope) {
				continue
			}
			out = append(out, scope)
		}
	}
	return out
}

// detectAuthType scans auth paragraphs for phrases identifying token type.
func detectAuthType(lines []string) AuthType {
	joined := strings.ToLower(strings.Join(lines, "\n"))
	user := strings.Contains(joined, "user access token")
	app := strings.Contains(joined, "app access token")
	switch {
	case user && app:
		return AuthEitherToken
	case user:
		return AuthUserToken
	case app:
		return AuthAppToken
	default:
		return AuthAnonymous
	}
}

func isDeprecated(description, summary string) bool {
	hay := description + "\n" + summary
	for _, txt := range []string{
		"DEPRECATED",
		"As of February 28, 2023",
	} {
		if strings.Contains(hay, txt) {
			return true
		}
	}
	return false
}
