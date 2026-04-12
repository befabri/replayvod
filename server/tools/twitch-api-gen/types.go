package main

// FieldSchema is the raw result of parsing one `<tr>` in a Twitch docs table.
// Types/required are preserved as written by Twitch; the generator translates later.
type FieldSchema struct {
	Name        string
	Type        string // raw Twitch type, e.g. "String", "Object[]"
	Required    *bool  // nil = not stated
	Description string
	Depth       int
	EnumValues  []any // string or int
	EnumDefault any
	Children    []FieldSchema

	// Validate and ValidateDive are go-playground/validator tag fragments
	// extracted from Description by constraints.go. Validate applies to the
	// field itself; ValidateDive applies to each element of an array field.
	// Populated for request-side fields (Params, Body) only.
	Validate     string
	ValidateDive string
}

// AuthType is the Twitch authentication requirement for an endpoint.
type AuthType int

const (
	AuthAnonymous AuthType = iota
	AuthUserToken
	AuthAppToken
	AuthEitherToken
)

func (a AuthType) String() string {
	switch a {
	case AuthUserToken:
		return "user"
	case AuthAppToken:
		return "app"
	case AuthEitherToken:
		return "either"
	default:
		return "anonymous"
	}
}

// StatusCode is a row in the Response Codes table.
type StatusCode struct {
	Code        int
	Description string
}

// EndpointMeta is one row in the master `#twitch-api-reference` summary table.
type EndpointMeta struct {
	ID      string
	Tag     string
	Summary string
	Name    string // link text, e.g. "Get Users"
}

// EndpointDef is the full parsed form of one Twitch Helix endpoint.
type EndpointDef struct {
	ID          string
	Name        string
	Tag         string
	Summary     string
	Description string
	Method      string
	Path        string
	AuthType    AuthType
	Scopes      []string
	QueryParams []FieldSchema
	BodyFields  []FieldSchema
	Response    []FieldSchema
	StatusCodes []StatusCode
	Deprecated  bool
}
