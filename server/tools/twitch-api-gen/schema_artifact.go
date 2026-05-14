package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const normalizedSchemaVersion = 1

// normalizedSchema is the scraper/generator boundary. The scraper owns keeping
// this JSON fixture in sync with Twitch docs; the generator consumes it so code
// changes can be reviewed independently from docs drift.
type normalizedSchema struct {
	Version               int                        `json:"version"`
	SourceURL             string                     `json:"source_url"`
	Endpoints             []EndpointDef              `json:"endpoints"`
	EventSubReference     *EventSubReference         `json:"eventsub_reference,omitempty"`
	EventSubSubscriptions []EventSubSubscriptionType `json:"eventsub_subscriptions,omitempty"`
}

type normalizedSchemaJSON struct {
	Version               int                              `json:"version"`
	SourceURL             string                           `json:"source_url"`
	Endpoints             []endpointSchemaJSON             `json:"endpoints"`
	EventSubReference     *eventSubReferenceSchemaJSON     `json:"eventsub_reference,omitempty"`
	EventSubSubscriptions []eventSubSubscriptionSchemaJSON `json:"eventsub_subscriptions,omitempty"`
}

type endpointSchemaJSON struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Tag         string            `json:"tag,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Method      string            `json:"method,omitempty"`
	Path        string            `json:"path,omitempty"`
	AuthType    string            `json:"auth_type"`
	Scopes      []string          `json:"scopes,omitempty"`
	QueryParams []fieldSchemaJSON `json:"query_params,omitempty"`
	BodyFields  []fieldSchemaJSON `json:"body_fields,omitempty"`
	Response    []fieldSchemaJSON `json:"response,omitempty"`
	StatusCodes []statusCodeJSON  `json:"status_codes,omitempty"`
	Deprecated  bool              `json:"deprecated,omitempty"`
}

type fieldSchemaJSON struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Required     *bool             `json:"required,omitempty"`
	Description  string            `json:"description,omitempty"`
	Depth        int               `json:"depth,omitempty"`
	EnumValues   []any             `json:"enum_values,omitempty"`
	EnumDefault  any               `json:"enum_default,omitempty"`
	Children     []fieldSchemaJSON `json:"children,omitempty"`
	Validate     string            `json:"validate,omitempty"`
	ValidateDive string            `json:"validate_dive,omitempty"`
}

type statusCodeJSON struct {
	Code        int    `json:"code"`
	Description string `json:"description,omitempty"`
}

type eventSubReferenceSchemaJSON struct {
	Conditions   map[string]eventSubSchemaJSON `json:"conditions,omitempty"`
	Events       map[string]eventSubSchemaJSON `json:"events,omitempty"`
	NamedSchemas map[string]eventSubSchemaJSON `json:"named_schemas,omitempty"`
}

type eventSubSchemaJSON struct {
	AnchorID string            `json:"anchor_id"`
	Fields   []fieldSchemaJSON `json:"fields,omitempty"`
}

type eventSubSubscriptionSchemaJSON struct {
	Type            string `json:"type"`
	Version         string `json:"version"`
	MasterAnchor    string `json:"master_anchor,omitempty"`
	ConditionAnchor string `json:"condition_anchor,omitempty"`
	EventAnchor     string `json:"event_anchor,omitempty"`
}

func (s normalizedSchema) MarshalJSON() ([]byte, error) {
	return json.Marshal(toNormalizedSchemaJSON(s))
}

func (s *normalizedSchema) UnmarshalJSON(b []byte) error {
	var wire normalizedSchemaJSON
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	schema, err := fromNormalizedSchemaJSON(wire)
	if err != nil {
		return err
	}
	*s = schema
	return nil
}

func buildNormalizedSchema(sourceURL string, defs []EndpointDef, eventSubRef *EventSubReference, eventSubSubs []EventSubSubscriptionType) normalizedSchema {
	sortedDefs := make([]EndpointDef, len(defs))
	copy(sortedDefs, defs)
	slices.SortFunc(sortedDefs, func(a, b EndpointDef) int {
		return strings.Compare(a.ID, b.ID)
	})

	sortedEventSubSubs := make([]EventSubSubscriptionType, len(eventSubSubs))
	copy(sortedEventSubSubs, eventSubSubs)
	slices.SortFunc(sortedEventSubSubs, func(a, b EventSubSubscriptionType) int {
		if c := strings.Compare(a.Type, b.Type); c != 0 {
			return c
		}
		return strings.Compare(a.Version, b.Version)
	})

	return normalizedSchema{
		Version:               normalizedSchemaVersion,
		SourceURL:             sourceURL,
		Endpoints:             sortedDefs,
		EventSubReference:     eventSubRef,
		EventSubSubscriptions: sortedEventSubSubs,
	}
}

func toNormalizedSchemaJSON(schema normalizedSchema) normalizedSchemaJSON {
	return normalizedSchemaJSON{
		Version:               schema.Version,
		SourceURL:             schema.SourceURL,
		Endpoints:             endpointsToJSON(schema.Endpoints),
		EventSubReference:     eventSubReferenceToJSON(schema.EventSubReference),
		EventSubSubscriptions: eventSubSubscriptionsToJSON(schema.EventSubSubscriptions),
	}
}

func fromNormalizedSchemaJSON(wire normalizedSchemaJSON) (normalizedSchema, error) {
	endpoints, err := endpointsFromJSON(wire.Endpoints)
	if err != nil {
		return normalizedSchema{}, err
	}
	return normalizedSchema{
		Version:               wire.Version,
		SourceURL:             wire.SourceURL,
		Endpoints:             endpoints,
		EventSubReference:     eventSubReferenceFromJSON(wire.EventSubReference),
		EventSubSubscriptions: eventSubSubscriptionsFromJSON(wire.EventSubSubscriptions),
	}, nil
}

func endpointsToJSON(endpoints []EndpointDef) []endpointSchemaJSON {
	out := make([]endpointSchemaJSON, 0, len(endpoints))
	for _, ep := range endpoints {
		statusCodes := make([]statusCodeJSON, 0, len(ep.StatusCodes))
		for _, code := range ep.StatusCodes {
			statusCodes = append(statusCodes, statusCodeJSON{Code: code.Code, Description: code.Description})
		}
		out = append(out, endpointSchemaJSON{
			ID:          ep.ID,
			Name:        ep.Name,
			Tag:         ep.Tag,
			Summary:     ep.Summary,
			Description: ep.Description,
			Method:      ep.Method,
			Path:        ep.Path,
			AuthType:    ep.AuthType.String(),
			Scopes:      ep.Scopes,
			QueryParams: fieldsToJSON(ep.QueryParams),
			BodyFields:  fieldsToJSON(ep.BodyFields),
			Response:    fieldsToJSON(ep.Response),
			StatusCodes: statusCodes,
			Deprecated:  ep.Deprecated,
		})
	}
	return out
}

func endpointsFromJSON(endpoints []endpointSchemaJSON) ([]EndpointDef, error) {
	out := make([]EndpointDef, 0, len(endpoints))
	for _, ep := range endpoints {
		authType, err := parseAuthType(ep.AuthType)
		if err != nil {
			return nil, fmt.Errorf("endpoint %q: %w", ep.ID, err)
		}
		statusCodes := make([]StatusCode, 0, len(ep.StatusCodes))
		for _, code := range ep.StatusCodes {
			statusCodes = append(statusCodes, StatusCode{Code: code.Code, Description: code.Description})
		}
		out = append(out, EndpointDef{
			ID:          ep.ID,
			Name:        ep.Name,
			Tag:         ep.Tag,
			Summary:     ep.Summary,
			Description: ep.Description,
			Method:      ep.Method,
			Path:        ep.Path,
			AuthType:    authType,
			Scopes:      ep.Scopes,
			QueryParams: fieldsFromJSON(ep.QueryParams),
			BodyFields:  fieldsFromJSON(ep.BodyFields),
			Response:    fieldsFromJSON(ep.Response),
			StatusCodes: statusCodes,
			Deprecated:  ep.Deprecated,
		})
	}
	return out, nil
}

func fieldsToJSON(fields []FieldSchema) []fieldSchemaJSON {
	out := make([]fieldSchemaJSON, 0, len(fields))
	for _, field := range fields {
		out = append(out, fieldSchemaJSON{
			Name:         field.Name,
			Type:         field.Type,
			Required:     field.Required,
			Description:  field.Description,
			Depth:        field.Depth,
			EnumValues:   field.EnumValues,
			EnumDefault:  field.EnumDefault,
			Children:     fieldsToJSON(field.Children),
			Validate:     field.Validate,
			ValidateDive: field.ValidateDive,
		})
	}
	return out
}

func fieldsFromJSON(fields []fieldSchemaJSON) []FieldSchema {
	out := make([]FieldSchema, 0, len(fields))
	for _, field := range fields {
		out = append(out, FieldSchema{
			Name:         field.Name,
			Type:         field.Type,
			Required:     field.Required,
			Description:  field.Description,
			Depth:        field.Depth,
			EnumValues:   field.EnumValues,
			EnumDefault:  field.EnumDefault,
			Children:     fieldsFromJSON(field.Children),
			Validate:     field.Validate,
			ValidateDive: field.ValidateDive,
		})
	}
	return out
}

func eventSubReferenceToJSON(ref *EventSubReference) *eventSubReferenceSchemaJSON {
	if ref == nil {
		return nil
	}
	return &eventSubReferenceSchemaJSON{
		Conditions:   eventSubSchemasToJSON(ref.Conditions),
		Events:       eventSubSchemasToJSON(ref.Events),
		NamedSchemas: eventSubSchemasToJSON(ref.NamedSchemas),
	}
}

func eventSubReferenceFromJSON(ref *eventSubReferenceSchemaJSON) *EventSubReference {
	if ref == nil {
		return nil
	}
	return &EventSubReference{
		Conditions:   eventSubSchemasFromJSON(ref.Conditions),
		Events:       eventSubSchemasFromJSON(ref.Events),
		NamedSchemas: eventSubSchemasFromJSON(ref.NamedSchemas),
	}
}

func eventSubSchemasToJSON(schemas map[string]EventSubSchema) map[string]eventSubSchemaJSON {
	if schemas == nil {
		return nil
	}
	out := make(map[string]eventSubSchemaJSON, len(schemas))
	for key, schema := range schemas {
		out[key] = eventSubSchemaJSON{AnchorID: schema.AnchorID, Fields: fieldsToJSON(schema.Fields)}
	}
	return out
}

func eventSubSchemasFromJSON(schemas map[string]eventSubSchemaJSON) map[string]EventSubSchema {
	if schemas == nil {
		return nil
	}
	out := make(map[string]EventSubSchema, len(schemas))
	for key, schema := range schemas {
		out[key] = EventSubSchema{AnchorID: schema.AnchorID, Fields: fieldsFromJSON(schema.Fields)}
	}
	return out
}

func eventSubSubscriptionsToJSON(subs []EventSubSubscriptionType) []eventSubSubscriptionSchemaJSON {
	out := make([]eventSubSubscriptionSchemaJSON, 0, len(subs))
	for _, sub := range subs {
		out = append(out, eventSubSubscriptionSchemaJSON{
			Type:            sub.Type,
			Version:         sub.Version,
			MasterAnchor:    sub.MasterAnchor,
			ConditionAnchor: sub.ConditionAnchor,
			EventAnchor:     sub.EventAnchor,
		})
	}
	return out
}

func eventSubSubscriptionsFromJSON(subs []eventSubSubscriptionSchemaJSON) []EventSubSubscriptionType {
	out := make([]EventSubSubscriptionType, 0, len(subs))
	for _, sub := range subs {
		out = append(out, EventSubSubscriptionType{
			Type:            sub.Type,
			Version:         sub.Version,
			MasterAnchor:    sub.MasterAnchor,
			ConditionAnchor: sub.ConditionAnchor,
			EventAnchor:     sub.EventAnchor,
		})
	}
	return out
}

func parseAuthType(s string) (AuthType, error) {
	switch s {
	case "", "anonymous":
		return AuthAnonymous, nil
	case "user":
		return AuthUserToken, nil
	case "app":
		return AuthAppToken, nil
	case "either":
		return AuthEitherToken, nil
	default:
		return AuthAnonymous, fmt.Errorf("unknown auth_type %q", s)
	}
}

func readNormalizedSchema(path string) (normalizedSchema, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return normalizedSchema{}, fmt.Errorf("read normalized schema: %w", err)
	}
	var schema normalizedSchema
	if err := json.Unmarshal(b, &schema); err != nil {
		return normalizedSchema{}, fmt.Errorf("decode normalized schema: %w", err)
	}
	if schema.Version != normalizedSchemaVersion {
		return normalizedSchema{}, fmt.Errorf("unsupported normalized schema version %d", schema.Version)
	}
	return schema, nil
}

func writeNormalizedSchema(path string, schema normalizedSchema) error {
	b, err := marshalNormalizedSchema(schema)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func marshalNormalizedSchema(schema normalizedSchema) ([]byte, error) {
	b, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal normalized schema: %w", err)
	}
	return append(b, '\n'), nil
}

func computeSourceHash(sourceURL string, defs []EndpointDef, eventSubRef *EventSubReference, eventSubSubs []EventSubSubscriptionType) (string, error) {
	return sourceHashForNormalizedSchema(buildNormalizedSchema(sourceURL, defs, eventSubRef, eventSubSubs))
}

func sourceHashForNormalizedSchema(schema normalizedSchema) (string, error) {
	b, err := marshalNormalizedSchema(schema)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
