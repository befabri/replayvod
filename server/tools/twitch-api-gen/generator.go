package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

// GenerateOptions controls code emission.
type GenerateOptions struct {
	OutDir    string
	SourceURL string
	// Timestamp is the value used in the generated-file header. When zero, uses
	// the current UTC time. Tests and snapshot-driven runs should pass a fixed
	// value (e.g. the cache file mtime) so the output is reproducible.
	Timestamp time.Time
	// Optional EventSub scraper output; when present, the generator emits
	// generated_eventsub.go with typed Condition/Event overlays and rewires
	// the Condition field on EventSubSubscription / CreateEventSubSubscriptionBody.
	EventSubReference *EventSubReference
	EventSubSubs      []EventSubSubscriptionType
	Log               *slog.Logger
}

// Generate writes generated_types.go and generated_client.go into opts.OutDir.
func Generate(defs []EndpointDef, opts GenerateOptions) error {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	model, err := buildModel(defs, opts.SourceURL, ts, opts.Log)
	if err != nil {
		return err
	}

	hasEventSub := opts.EventSubReference != nil && len(opts.EventSubSubs) > 0
	if hasEventSub {
		BuildEventSubModel(opts.EventSubReference, opts.EventSubSubs, model, opts.Log)
	}

	if err := renderAndWrite("types.go.tmpl", filepath.Join(opts.OutDir, "generated_types.go"), model); err != nil {
		return err
	}
	if err := renderAndWrite("client.go.tmpl", filepath.Join(opts.OutDir, "generated_client.go"), model); err != nil {
		return err
	}
	if hasEventSub {
		if err := renderAndWrite("eventsub.go.tmpl", filepath.Join(opts.OutDir, "generated_eventsub.go"), model); err != nil {
			return err
		}
	}
	return nil
}

func renderAndWrite(tmplName, outPath string, data any) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/"+tmplName)
	if err != nil {
		return fmt.Errorf("parse template %s: %w", tmplName, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template %s: %w", tmplName, err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("gofmt %s: %w\n--- generated ---\n%s", outPath, err, buf.String())
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, formatted, 0o644)
}

// --- model: per-template view of the parsed endpoints ---

type templateModel struct {
	SourceURL          string
	Timestamp          string
	ImportTime         bool
	Types              []typeModel
	ParamTypes         []typeModel
	BodyTypes          []typeModel
	Endpoints          []endpointModel
	EventSubConditions []eventSubTypeModel
	EventSubEvents     []eventSubTypeModel
	EventSubDispatch   eventSubDispatchModel
}

// eventSubTypeModel is one scraped EventSub schema to emit as a Go struct.
// Nested types — produced by recursing into Object/Object[] children of a
// condition or event schema — also use this model but set Nested=true so the
// template emits a different header comment and skips the sealed-interface
// marker method (nested types aren't EventSubCondition/EventSubEvent themselves).
type eventSubTypeModel struct {
	GoName   string // e.g. "ChannelFollowCondition" or "ChannelChatMessageEventMessage"
	AnchorID string // e.g. "channel-follow-condition" (empty for Nested)
	Fields   []fieldModel
	Nested   bool
}

// eventSubDispatchModel holds the switch-case data for generated factories.
type eventSubDispatchModel struct {
	ConditionCases []eventSubCase
	EventCases     []eventSubCase
}

type eventSubCase struct {
	TypeString string // "channel.follow"
	Version    string // "2"
	GoName     string // "ChannelFollowCondition"
}

type typeModel struct {
	Name       string
	SourceID   string // endpoint ID responsible for this type (response types)
	EndpointID string // for param and body types
	Fields     []fieldModel
	Nested     bool // true for anonymous-but-named nested structs
}

type fieldModel struct {
	GoName     string
	GoType     string
	JSONName   string
	OmitEmpty  bool
	Deprecated bool // emits a `// Deprecated: …` doc comment before the field

	// Required, Validate, ValidateDive are request-side only (Params + Body).
	// Response types leave these empty so the template doesn't emit a tag.
	Required     bool
	Validate     string
	ValidateDive string
	// ValidateTag is the pre-composed tag contents (without the `validate:""`
	// wrapper) — empty when no constraint was extracted and required/omitempty
	// would be the sole content.
	ValidateTag string
}

// composeValidateTag assembles a go-playground/validator tag fragment.
// Returns empty when there's nothing to assert — an optional field with no
// extracted constraint produces no tag (bare omitempty is noise). But a
// required field ALWAYS gets at least `required`, even without other constraints.
//
// `goType` is consulted to suppress semantically-incorrect tags: `required` on
// a `bool` rejects `false` as the zero value, but `false` is a valid explicit
// request payload (e.g. `is_enabled: false` disables a feature). Validator
// can't distinguish "caller set it to false" from "caller didn't set it", so
// we drop `required` for bools entirely and trust the caller.
func composeValidateTag(forRequest, required bool, goType, validate, validateDive string) string {
	if !forRequest {
		return ""
	}
	if goType == "bool" && required {
		required = false
	}
	if !required && validate == "" && validateDive == "" {
		return ""
	}
	var parts []string
	if required {
		parts = append(parts, "required")
	} else {
		parts = append(parts, "omitempty")
	}
	if validate != "" {
		parts = append(parts, validate)
	}
	if validateDive != "" {
		parts = append(parts, "dive", validateDive)
	}
	return strings.Join(parts, ",")
}

type endpointModel struct {
	ID           string
	Summary      string
	HTTPMethod   string
	HTTPHelper   string // get / post / delete
	Path         string
	MethodName   string
	AuthType     string
	Scopes       []string
	ScopesJoined string
	HasParams    bool
	ParamsType   string
	HasBody      bool
	BodyType     string
	ItemType     string
	Paginated    bool
	NoContent    bool // 204 endpoint — method returns only `error`
	ReturnType   string
	OKReturn     string
	ErrReturn    string
}

func buildModel(defs []EndpointDef, sourceURL string, timestamp time.Time, log *slog.Logger) (*templateModel, error) {
	model := &templateModel{
		SourceURL: sourceURL,
		Timestamp: timestamp.UTC().Format(time.RFC3339),
	}

	// Sort defs by ID for deterministic output.
	sorted := make([]EndpointDef, len(defs))
	copy(sorted, defs)
	slices.SortFunc(sorted, func(a, b EndpointDef) int {
		return strings.Compare(a.ID, b.ID)
	})

	emittedTypes := map[string]bool{}

	for _, ep := range sorted {
		dataField, paginated := splitListResponse(ep.Response)
		noContent := dataField == nil
		if noContent && !endpointHas2xx(ep.StatusCodes) {
			log.Warn("generator: skipping endpoint with no recognisable response shape", "endpoint", ep.ID)
			continue
		}

		var itemTypeName string
		if !noContent {
			itemTypeName = responseItemType(ep.ID)
			if itemTypeName == "" {
				itemTypeName = PascalCase(ep.ID) + "Response"
				log.Warn("generator: no schema name mapping; using fallback", "endpoint", ep.ID, "type", itemTypeName)
			}

			if !emittedTypes[itemTypeName] {
				emittedTypes[itemTypeName] = true
				tm, err := buildStructType(itemTypeName, ep.ID, dataField.Children, &model.Types, emittedTypes, model, false, log)
				if err != nil {
					return nil, fmt.Errorf("endpoint %q: %w", ep.ID, err)
				}
				model.Types = append(model.Types, tm)
			}
		}

		// Params struct.
		var paramsType string
		hasParams := len(ep.QueryParams) > 0
		if hasParams {
			paramsType = PascalCase(ep.ID) + "Params"
			pm := typeModel{Name: paramsType, EndpointID: ep.ID}
			for _, q := range ep.QueryParams {
				fm, err := toParamFieldModel(ep.ID, q, log)
				if err != nil {
					return nil, fmt.Errorf("endpoint %q param %q: %w", ep.ID, q.Name, err)
				}
				pm.Fields = append(pm.Fields, fm)
			}
			if paginated && !hasJSONField(pm.Fields, "after") {
				pm.Fields = append(pm.Fields, fieldModel{
					GoName: "After", GoType: "string", JSONName: "after", OmitEmpty: true,
				})
			}
			model.ParamTypes = append(model.ParamTypes, pm)
		}

		// Body struct (JSON).
		var bodyType string
		hasBody := len(ep.BodyFields) > 0
		if hasBody {
			bodyType = PascalCase(ep.ID) + "Body"
			bm, err := buildStructType(bodyType, ep.ID, ep.BodyFields, &model.BodyTypes, emittedTypes, model, true, log)
			if err != nil {
				return nil, fmt.Errorf("endpoint %q body: %w", ep.ID, err)
			}
			bm.EndpointID = ep.ID
			bm.SourceID = ""
			model.BodyTypes = append(model.BodyTypes, bm)
		}

		epm := endpointModel{
			ID:           ep.ID,
			Summary:      ep.Summary,
			HTTPMethod:   ep.Method,
			HTTPHelper:   strings.ToLower(ep.Method),
			Path:         ep.Path,
			MethodName:   MethodName(ep.ID),
			AuthType:     ep.AuthType.String(),
			Scopes:       ep.Scopes,
			ScopesJoined: strings.Join(ep.Scopes, ", "),
			HasParams:    hasParams,
			ParamsType:   paramsType,
			HasBody:      hasBody,
			BodyType:     bodyType,
			ItemType:     itemTypeName,
			Paginated:    paginated,
			NoContent:    noContent,
		}
		switch {
		case noContent:
			epm.ReturnType = "error"
			epm.OKReturn = "nil"
			epm.ErrReturn = "err"
		case paginated:
			epm.ReturnType = "[]" + itemTypeName + ", Pagination, error"
			epm.OKReturn = "result.Data, Pagination{Cursor: result.Pagination.Cursor, Total: result.Total, TotalCost: result.TotalCost, MaxCost: result.MaxCost}, nil"
			epm.ErrReturn = "nil, Pagination{}, err"
		default:
			epm.ReturnType = "[]" + itemTypeName + ", error"
			epm.OKReturn = "result.Data, nil"
			epm.ErrReturn = "nil, err"
		}
		model.Endpoints = append(model.Endpoints, epm)
	}
	return model, nil
}

// buildStructType produces a typeModel for `name` using the given children.
// Nested Object/Object[] children with their own children emit additional
// typeModels (appended to `types`) named `{name}{FieldPascalCase}` (singularized
// for arrays). Mutates model.ImportTime when a field resolves to time.Time.
func buildStructType(
	name, sourceID string,
	children []FieldSchema,
	types *[]typeModel,
	emitted map[string]bool,
	model *templateModel,
	forRequest bool,
	log *slog.Logger,
) (typeModel, error) {
	tm := typeModel{Name: name, SourceID: sourceID}
	for _, child := range children {
		fm, err := toStructFieldModel(name, child, types, emitted, model, forRequest, log)
		if err != nil {
			return typeModel{}, fmt.Errorf("field %q: %w", child.Name, err)
		}
		tm.Fields = append(tm.Fields, fm)
		if strings.Contains(fm.GoType, "time.Time") {
			model.ImportTime = true
		}
	}
	return tm, nil
}

// deprecatedFieldMarkers lists description substrings Twitch uses to mark a
// struct field deprecated. Ported from FIELD_DEPRECATED_TEXT in the TS reference
// plus the plain "**DEPRECATED**" inline marker seen in EventSub docs.
var deprecatedFieldMarkers = []string{
	"**DEPRECATED**",
	"**IMPORTANT** As of February 28, 2023, this field is deprecated",
	"**NOTE**: This field has been deprecated",
	"This field has been deprecated",
}

func isDeprecatedField(description string) bool {
	for _, m := range deprecatedFieldMarkers {
		if strings.Contains(description, m) {
			return true
		}
	}
	return false
}

// toStructFieldModel handles one field inside a struct. When the field is an
// Object/Object[] with children, a nested struct is generated and its name is
// used as the field's Go type.
func toStructFieldModel(
	parent string, f FieldSchema,
	types *[]typeModel, emitted map[string]bool,
	model *templateModel, forRequest bool, log *slog.Logger,
) (fieldModel, error) {
	// Condition/Transport fields are polymorphic; route to the generated sealed
	// interfaces so callers can type-switch instead of decoding any. Both
	// preserve the Required flag from the parsed schema so pre-flight validation
	// catches nil interface values on required request bodies.
	if (f.Name == "condition" || f.Name == "transport") &&
		(parent == "EventSubSubscription" || parent == "CreateEventSubSubscriptionBody") {
		goName := "Condition"
		goType := "EventSubCondition"
		if f.Name == "transport" {
			goName = "Transport"
			goType = "EventSubTransport"
		}
		required := forRequest && f.Required != nil && *f.Required
		return fieldModel{
			GoName:      goName,
			GoType:      goType,
			JSONName:    f.Name,
			Required:    required,
			ValidateTag: composeValidateTag(forRequest, required, goType, "", ""),
		}, nil
	}
	goType := ""
	hasChildren := len(f.Children) > 0

	if hasChildren {
		isArrayType := strings.HasSuffix(f.Type, "[]")
		nestedName := parent + PascalCase(f.Name)
		if isArrayType {
			nestedName = parent + PascalCase(singularize(f.Name))
		}
		if !emitted[nestedName] {
			emitted[nestedName] = true
			nested, err := buildStructType(nestedName, "", f.Children, types, emitted, model, forRequest, log)
			if err != nil {
				return fieldModel{}, err
			}
			nested.Nested = true
			*types = append(*types, nested)
		}
		if isArrayType {
			goType = "[]" + nestedName
		} else {
			goType = nestedName
		}
	} else {
		goType = GoType(f, "")
		if goType == "any" || goType == "[]any" {
			log.Warn("generator: unknown/unmapped type", "name", f.Name, "type", f.Type, "parent", parent)
		}
	}

	omitEmpty := f.Required == nil || !*f.Required
	fm := fieldModel{
		GoName:     PascalCase(f.Name),
		GoType:     goType,
		JSONName:   f.Name,
		OmitEmpty:  omitEmpty,
		Deprecated: isDeprecatedField(f.Description),
	}
	if forRequest {
		fm.Required = f.Required != nil && *f.Required
		fm.Validate = f.Validate
		fm.ValidateDive = f.ValidateDive
		fm.ValidateTag = composeValidateTag(true, fm.Required, fm.GoType, fm.Validate, fm.ValidateDive)
	}
	return fm, nil
}

// mutuallyExclusiveParamEndpoints lists endpoints where Twitch marks several
// query parameters `Required: Yes` but only ONE needs to be supplied per
// request (e.g. get-games accepts `id` OR `name` OR `igdb_id`). Ported from
// parseSchemaObject.ts. For these endpoints the generator downgrades the
// required flag on every param to optional so a valid `id=123` request isn't
// rejected client-side for missing `name`.
var mutuallyExclusiveParamEndpoints = map[string]bool{
	"get-clips":          true,
	"get-stream-markers": true,
	"get-teams":          true,
	"get-videos":         true,
	"get-games":          true,
}

// toParamFieldModel converts a query parameter into an emitted struct field.
// Scalar fields get `,omitempty`. Array parameters (Twitch `?id=A&id=B`
// convention) become `[]T` without omitempty since nil slices serialize as nothing.
func toParamFieldModel(endpointID string, f FieldSchema, log *slog.Logger) (fieldModel, error) {
	goType := GoType(f, "")
	if goType == "any" || goType == "[]any" {
		log.Warn("generator: unknown param type", "name", f.Name, "type", f.Type)
	}
	isArray := IsArrayParam(f.Description)
	omitEmpty := true
	if isArray {
		if !strings.HasPrefix(goType, "[]") {
			goType = "[]" + goType
		}
		omitEmpty = false
	}
	required := f.Required != nil && *f.Required
	if mutuallyExclusiveParamEndpoints[endpointID] {
		required = false
	}
	return fieldModel{
		GoName:       PascalCase(f.Name),
		GoType:       goType,
		JSONName:     f.Name,
		OmitEmpty:    omitEmpty,
		Required:     required,
		Validate:     f.Validate,
		ValidateDive: f.ValidateDive,
		ValidateTag:  composeValidateTag(true, required, goType, f.Validate, f.ValidateDive),
	}, nil
}

// --- helpers ---

func splitListResponse(fields []FieldSchema) (*FieldSchema, bool) {
	var data *FieldSchema
	paginated := false
	for i := range fields {
		f := &fields[i]
		switch f.Name {
		case "data":
			if f.Type == "Object[]" || f.Type == "Object" {
				data = f
			}
		case "pagination":
			paginated = true
		}
	}
	return data, paginated
}

func endpointHas2xx(codes []StatusCode) bool {
	for _, c := range codes {
		if c.Code >= 200 && c.Code < 300 {
			return true
		}
	}
	return false
}

func hasJSONField(fields []fieldModel, name string) bool {
	for _, f := range fields {
		if f.JSONName == name {
			return true
		}
	}
	return false
}

// singularize strips a trailing 's' (but not 'ss') or 'ies' → 'y'. Mechanical
// English-only heuristic sufficient for Twitch's field naming.
func singularize(name string) string {
	if strings.HasSuffix(name, "ies") {
		return strings.TrimSuffix(name, "ies") + "y"
	}
	if strings.HasSuffix(name, "ss") {
		return name
	}
	if strings.HasSuffix(name, "s") {
		return strings.TrimSuffix(name, "s")
	}
	return name
}
