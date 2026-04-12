package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ErrNotSchemaTable is returned by ParseTable when the <table> doesn't look
// like a field-schema table (missing one of name/type/description headers).
// Callers can treat this as "skip this table" vs wrapping other errors as
// real parse failures.
var ErrNotSchemaTable = errors.New("not a schema table")

// ParseTable turns one Twitch docs `<table>` into a tree of FieldSchema.
// Ported from parseTableSchema.ts.
func ParseTable(table *goquery.Selection) ([]FieldSchema, error) {
	parameterIdx, typeIdx, requiredIdx, descriptionIdx := -1, -1, -1, -1
	table.Find("thead th").Each(func(i int, th *goquery.Selection) {
		txt := strings.ToLower(strings.TrimSpace(th.Text()))
		switch {
		case txt == "fields" || txt == "field" || txt == "parameter" || txt == "param" || txt == "name":
			parameterIdx = i
		case txt == "type":
			typeIdx = i
		case txt == "required?" || txt == "required":
			requiredIdx = i
		case txt == "description":
			descriptionIdx = i
		}
	})
	if parameterIdx == -1 || typeIdx == -1 || descriptionIdx == -1 {
		header := strings.TrimSpace(table.Find("thead").Text())
		return nil, fmt.Errorf("%w: headers %q", ErrNotSchemaTable, header)
	}

	var schemas []FieldSchema

	var parseErr error
	table.Find("tbody tr").EachWithBreak(func(_ int, tr *goquery.Selection) bool {
		cells := tr.Children() // <td> siblings
		if cells.Length() <= descriptionIdx {
			return true // skip malformed row
		}
		parameterCell := cells.Eq(parameterIdx)
		typeCell := cells.Eq(typeIdx)
		descriptionCell := cells.Eq(descriptionIdx)
		var requiredCell *goquery.Selection
		if requiredIdx != -1 && cells.Length() > requiredIdx {
			c := cells.Eq(requiredIdx)
			requiredCell = c
		}

		parameterText := parameterCell.Text()
		name := strings.TrimSpace(parameterText)
		typeText := strings.TrimSpace(typeCell.Text())
		descriptionText := descriptionCell.Text()
		descriptionLower := strings.ToLower(descriptionText)

		// Required: column value first; description overrides to false if it hints optional.
		// Twitch's Helix docs use "Yes"/"No"; EventSub docs use "yes"/"no". Accept either.
		var required *bool
		if requiredCell != nil {
			rt := strings.TrimSpace(requiredCell.Text())
			if rt != "" {
				v := strings.EqualFold(rt, "Yes")
				required = &v
			}
		}
		for _, hint := range []string{
			"required only if",
			"included only if",
			"this field only if",
			"if any.",
		} {
			if strings.Contains(descriptionLower, hint) {
				f := false
				required = &f
				break
			}
		}

		// Depth: leading U+00A0 or space chars, ceil(n/3), max 4.
		depth, err := nestingDepth(parameterText)
		if err != nil {
			parseErr = fmt.Errorf("field %q: %w", name, err)
			return false
		}

		// Parse description as plain text (no markdown translation — we don't need it).
		descriptionTrimmed := strings.TrimSpace(collapseWhitespace(descriptionText))

		// Enum
		var enumValues []any
		var enumDefault any
		if containsAny(descriptionLower, enumTriggers) {
			var rawValues []string
			descriptionCell.Find("ul li").Each(func(_ int, li *goquery.Selection) {
				text := li.Text()
				value := splitEnumValue(text)
				if value != "" {
					rawValues = append(rawValues, value)
				}
			})
			if len(rawValues) == 0 {
				if m := enumRegex.FindStringSubmatch(descriptionText); m != nil {
					values := m[1]
					if !strings.Contains(values, " through ") {
						for _, v := range strings.Split(values, ",") {
							v = strings.TrimSpace(v)
							if v != "" {
								rawValues = append(rawValues, v)
							}
						}
					}
				}
			}
			for _, v := range rawValues {
				if v == `""` {
					enumValues = append(enumValues, "")
					continue
				}
				isDefault := false
				if strings.HasSuffix(v, " (default)") {
					v = strings.TrimSuffix(v, " (default)")
					isDefault = true
				}
				if typeText == "Integer" {
					n, err := strconv.Atoi(strings.TrimSpace(v))
					if err == nil {
						enumValues = append(enumValues, n)
						if isDefault {
							enumDefault = n
						}
					} else {
						enumValues = append(enumValues, v)
						if isDefault {
							enumDefault = v
						}
					}
				} else {
					enumValues = append(enumValues, v)
					if isDefault {
						enumDefault = v
					}
				}
			}
		}

		field := FieldSchema{
			Name:        name,
			Type:        typeText,
			Required:    required,
			Description: descriptionTrimmed,
			Depth:       depth,
			EnumValues:  enumValues,
			EnumDefault: enumDefault,
		}
		addField(&schemas, field, depth)
		return true
	})

	return schemas, parseErr
}

// nestingDepth counts leading non-breaking-space (U+00A0) and ASCII-space characters
// and returns ceil(count/3). Max depth 4 is enforced as a sanity check.
func nestingDepth(cellText string) (int, error) {
	count := 0
	for _, r := range cellText {
		if r == '\u00a0' || r == ' ' {
			count++
			continue
		}
		break
	}
	depth := (count + 2) / 3 // ceil(count/3)
	if depth > 4 {
		return 0, fmt.Errorf("depth > 4 (count=%d)", count)
	}
	return depth, nil
}

// addField inserts field into schemas, descending into the last element's children
// for each depth level.
func addField(schemas *[]FieldSchema, field FieldSchema, depth int) {
	if depth == 0 {
		*schemas = append(*schemas, field)
		return
	}
	if len(*schemas) == 0 {
		// No parent row at depth-1; attach as top-level to avoid panic.
		*schemas = append(*schemas, field)
		return
	}
	last := &(*schemas)[len(*schemas)-1]
	addField(&last.Children, field, depth-1)
}

var enumTriggers = []string{
	"values are:",
	"formats are:",
	"sizes are:",
	"tiers are:",
	"themes are:",
	"following named color values",
	"following values",
}

// enumRegex captures the values after trigger phrases, up to the first period or newline.
var enumRegex = regexp.MustCompile(`(?:values are:|formats are:|sizes are:|tiers are:|themes are:|following named color values|following values)\s*([^.\n]+)`)

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// splitEnumValue takes a <li> text like "hello — description" and returns "hello".
func splitEnumValue(text string) string {
	// Split on the first em-dash, colon, or " - ".
	if idx := strings.Index(text, " \u2014 "); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	if idx := strings.Index(text, "\u2014"); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	if idx := strings.Index(text, ":"); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	if idx := strings.Index(text, " - "); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	return strings.TrimSpace(text)
}

var wsRegex = regexp.MustCompile("[ \t\n\r\u00a0]+")

func collapseWhitespace(s string) string {
	return strings.TrimSpace(wsRegex.ReplaceAllString(s, " "))
}
