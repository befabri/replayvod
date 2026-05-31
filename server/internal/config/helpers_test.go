package config

import (
	"strings"
	"testing"
	"time"
)

func TestRedactedConfigRedactsSensitiveEnvironmentFields(t *testing.T) {
	cfg := &Config{Env: Environment{
		PostgresPassword:         "pg-secret",
		SessionSecret:            "session-secret",
		TwitchSecret:             "twitch-secret",
		HMACSecret:               "hmac-secret",
		ServiceAccountOAuthToken: "refresh-token",
		RelayIngestURL:           "https://relay.example/u/token-secret-123456",
		RelaySubscribeURL:        "wss://relay.example/u/token-secret/subscribe",
		WebhookCallbackURL:       "https://replayvod.example/api/v1/webhook/callback",
		RelayLocalCallbackURL:    "http://127.0.0.1:8080/api/v1/webhook/callback",
	}}

	redacted := cfg.RedactedConfig()

	assertRedacted := func(name, got string) {
		t.Helper()
		if got != "[REDACTED]" {
			t.Fatalf("%s = %q, want [REDACTED]", name, got)
		}
	}

	assertRedacted("PostgresPassword", redacted.Env.PostgresPassword)
	assertRedacted("SessionSecret", redacted.Env.SessionSecret)
	assertRedacted("TwitchSecret", redacted.Env.TwitchSecret)
	assertRedacted("HMACSecret", redacted.Env.HMACSecret)
	assertRedacted("ServiceAccountOAuthToken", redacted.Env.ServiceAccountOAuthToken)
	assertRedacted("RelaySubscribeURL", redacted.Env.RelaySubscribeURL)

	if redacted.Env.RelayIngestURL != "https://relay.example/u/REDACTED" {
		t.Fatalf("RelayIngestURL = %q, want relay token redacted", redacted.Env.RelayIngestURL)
	}

	if redacted.Env.RelayLocalCallbackURL != cfg.Env.RelayLocalCallbackURL {
		t.Fatalf("RelayLocalCallbackURL was redacted; local callback URL is not a bearer secret")
	}
}

func TestRedactedConfigLeavesNonRelayWebhookCallbackURLReadable(t *testing.T) {
	cfg := &Config{Env: Environment{
		WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback",
	}}

	redacted := cfg.RedactedConfig()

	if redacted.Env.WebhookCallbackURL != cfg.Env.WebhookCallbackURL {
		t.Fatalf("WebhookCallbackURL = %q, want %q", redacted.Env.WebhookCallbackURL, cfg.Env.WebhookCallbackURL)
	}
}

func TestServerModeCallbackURLUsesRelayIngestURLOnlyInRelayModes(t *testing.T) {
	cfg := &Config{ServerMode: ServerModeConfig{
		Mode:               ServerModeRelay,
		RelayIngestURL:     "https://relay.example/u/token-secret-123456",
		WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback",
	}}
	if got := cfg.ServerModeCallbackURL(); got != cfg.ServerMode.RelayIngestURL {
		t.Fatalf("ServerModeCallbackURL(relay) = %q, want %q", got, cfg.ServerMode.RelayIngestURL)
	}

	cfg.ServerMode.Mode = ServerModeDirect
	if got := cfg.ServerModeCallbackURL(); got != cfg.ServerMode.WebhookCallbackURL {
		t.Fatalf("ServerModeCallbackURL(direct) = %q, want %q", got, cfg.ServerMode.WebhookCallbackURL)
	}
}

func TestGetAddressJoinsHostAndPort(t *testing.T) {
	cfg := &Config{Env: Environment{Host: "0.0.0.0", Port: 8080}}
	if got := cfg.GetAddress(); got != "0.0.0.0:8080" {
		t.Fatalf("GetAddress() = %q, want %q", got, "0.0.0.0:8080")
	}
}

// TestGetPostgresDSN pins the assembled connection string, including the
// percent-encoding url.UserPassword applies to credentials containing reserved
// characters and the sslmode query parameter. A plain build that concatenated
// fields would leave "@", "/", and ":" in the password unescaped and corrupt
// the DSN.
func TestGetPostgresDSN(t *testing.T) {
	cases := []struct {
		name string
		env  Environment
		want string
	}{
		{
			name: "plain credentials",
			env: Environment{
				PostgresUser:     "postgres",
				PostgresPassword: "secret",
				PostgresHost:     "127.0.0.1",
				PostgresPort:     5432,
				PostgresDatabase: "replayvod",
				PostgresSSLMode:  "disable",
			},
			want: "postgres://postgres:secret@127.0.0.1:5432/replayvod?sslmode=disable",
		},
		{
			name: "reserved characters in password are escaped",
			env: Environment{
				PostgresUser:     "rvod",
				PostgresPassword: "p@ss/w:rd",
				PostgresHost:     "db.internal",
				PostgresPort:     5433,
				PostgresDatabase: "replayvod",
				PostgresSSLMode:  "require",
			},
			want: "postgres://rvod:p%40ss%2Fw%3Ard@db.internal:5433/replayvod?sslmode=require",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Env: tc.env}
			if got := cfg.GetPostgresDSN(); got != tc.want {
				t.Fatalf("GetPostgresDSN() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRedactRelayURLToken exercises redactRelayURLToken directly so each guard
// asserts on its own: only a well-formed /u/<token> URL has its token replaced,
// while unparseable URLs, relative URLs, short paths, a non-"u" prefix, and a
// segment that is not a relay token are all returned verbatim. Trailing
// segments and the query string survive the rewrite.
func TestRedactRelayURLToken(t *testing.T) {
	const token = "token-secret-123456" // 19 chars, passes isRelayToken
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "redacts valid relay token", raw: "https://relay.example/u/" + token, want: "https://relay.example/u/REDACTED"},
		{name: "preserves trailing segments", raw: "https://relay.example/u/" + token + "/extra", want: "https://relay.example/u/REDACTED/extra"},
		{name: "preserves query string", raw: "https://relay.example/u/" + token + "?x=1", want: "https://relay.example/u/REDACTED?x=1"},
		{name: "leaves relative url unchanged", raw: "/u/" + token, want: "/u/" + token},
		{name: "leaves unparseable url unchanged", raw: "https://[", want: "https://["},
		{name: "leaves single-segment path unchanged", raw: "https://relay.example/u", want: "https://relay.example/u"},
		{name: "leaves non-u prefix unchanged", raw: "https://relay.example/x/" + token, want: "https://relay.example/x/" + token},
		{name: "leaves short token unchanged", raw: "https://relay.example/u/short", want: "https://relay.example/u/short"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactRelayURLToken(tc.raw); got != tc.want {
				t.Fatalf("redactRelayURLToken(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestIsRelayToken pins both halves of the relay-token rule: the 16..128 length
// bounds and the [A-Za-z0-9_-] character class. The length cases probe exactly
// 15/16/128/129 so neither boundary can be shifted, and each character class is
// asserted both in isolation (so the alternatives cannot collapse to a single
// AND) and at the codepoint immediately outside each allowed range, so neither
// the >= nor the <= can drift by one.
func TestIsRelayToken(t *testing.T) {
	// pad returns a length-valid token whose only questionable character is bad,
	// isolating the character-class check from the length check.
	pad := func(bad string) string { return strings.Repeat("a", 15) + bad }

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		// Length bounds.
		{name: "empty", value: "", want: false},
		{name: "one below min", value: strings.Repeat("a", 15), want: false},
		{name: "exactly min", value: strings.Repeat("a", 16), want: true},
		{name: "exactly max", value: strings.Repeat("a", 128), want: true},
		{name: "one above max", value: strings.Repeat("a", 129), want: false},

		// Each allowed class on its own — a token built from a single class must
		// still pass, which forces the alternatives to stay OR-ed together.
		{name: "all lowercase", value: strings.Repeat("a", 16), want: true},
		{name: "all uppercase", value: strings.Repeat("Z", 16), want: true},
		{name: "all digits", value: strings.Repeat("9", 16), want: true},
		{name: "all underscores", value: strings.Repeat("_", 16), want: true},
		{name: "all hyphens", value: strings.Repeat("-", 16), want: true},
		{name: "upper bound of each range present", value: "azAZ09azAZ09azAZ", want: true},

		// One codepoint just outside each allowed range — all must be rejected.
		{name: "below 'a' (backtick)", value: pad("`"), want: false},
		{name: "above 'z' (brace)", value: pad("{"), want: false},
		{name: "below 'A' (at sign)", value: pad("@"), want: false},
		{name: "above 'Z' (bracket)", value: pad("["), want: false},
		{name: "below '0' (slash)", value: pad("/"), want: false},
		{name: "above '9' (colon)", value: pad(":"), want: false},
		{name: "space rejected", value: pad(" "), want: false},
		{name: "dot rejected", value: pad("."), want: false},
		{name: "non-ascii rejected", value: pad("é"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRelayToken(tc.value); got != tc.want {
				t.Fatalf("isRelayToken(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestParseOrigin covers the scheme+host extraction that gates whether a signed
// download URL can be built at all: a "" result means "no usable origin, omit
// the URL". The error/empty branches are the load-bearing ones — a regression
// returning a partial or wrong origin would emit broken links to an external
// consumer.
func TestParseOrigin(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"scheme and host only", "https://api.example.com", "https://api.example.com"},
		{"strips path query fragment", "https://api.example.com/a/b?c=d#e", "https://api.example.com"},
		{"keeps explicit port", "https://h:8443/cb", "https://h:8443"},
		{"derives from callback path", "http://localhost:8080/api/v1/auth/twitch/callback", "http://localhost:8080"},
		{"missing scheme", "//host/x", ""},
		{"missing host", "mailto:a@b.com", ""},
		{"scheme no host", "https:///just/path", ""},
		{"unparseable", "https://[", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseOrigin(tc.raw); got != tc.want {
				t.Errorf("parseOrigin(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestPublicAPIBaseURL pins the precedence the signed-URL feature depends on:
// an explicit PUBLIC_BASE_URL wins, app-managed/env direct mode can provide the
// public webhook callback origin, then OAuth CallbackURL is the fallback.
func TestPublicAPIBaseURL(t *testing.T) {
	cases := []struct {
		name        string
		publicBase  string
		callbackURL string
		serverMode  ServerModeConfig
		want        string
	}{
		{
			name:        "public base url wins over callback",
			publicBase:  "https://cdn.example.com",
			callbackURL: "https://api.example.com/cb",
			serverMode:  ServerModeConfig{Mode: ServerModeDirect, WebhookCallbackURL: "https://direct.example.com/api/v1/webhook/callback"},
			want:        "https://cdn.example.com",
		},
		{
			name:        "direct mode callback wins over oauth callback",
			publicBase:  "",
			callbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
			serverMode:  ServerModeConfig{Mode: ServerModeDirect, WebhookCallbackURL: "https://direct.example.com/api/v1/webhook/callback"},
			want:        "https://direct.example.com",
		},
		{
			name:        "falls back to callback origin when public base empty",
			publicBase:  "",
			callbackURL: "https://api.example.com/api/v1/auth/twitch/callback",
			want:        "https://api.example.com",
		},
		{
			name:        "falls through unusable public base and direct callback to oauth callback",
			publicBase:  "not-a-url",
			callbackURL: "https://api.example.com/cb",
			serverMode:  ServerModeConfig{Mode: ServerModeDirect, WebhookCallbackURL: "not-a-url"},
			want:        "https://api.example.com",
		},
		{
			name:        "relay ingest is not used as the public api origin",
			publicBase:  "",
			callbackURL: "https://api.example.com/cb",
			serverMode:  ServerModeConfig{Mode: ServerModeRelay, RelayIngestURL: "https://relay.example.com/u/token"},
			want:        "https://api.example.com",
		},
		{
			name:        "empty when neither yields a usable origin",
			publicBase:  "",
			callbackURL: "",
			want:        "",
		},
		{
			name:        "derives loopback default callback (documented dev behavior)",
			publicBase:  "",
			callbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
			want:        "http://localhost:8080",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{}
			c.Env.PublicBaseURL = tc.publicBase
			c.Env.CallbackURL = tc.callbackURL
			c.ServerMode = tc.serverMode
			if got := c.PublicAPIBaseURL(); got != tc.want {
				t.Errorf("PublicAPIBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOriginIsLoopback covers the safety check that warns when signed download
// links would point at localhost. Loopback hosts (and the localhost literal)
// must be flagged; a real public host and an empty/unparseable origin must not.
func TestOriginIsLoopback(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		want   bool
	}{
		{"localhost literal", "http://localhost:8080", true},
		{"127.0.0.1", "http://127.0.0.1:8080", true},
		{"127.x loopback range", "http://127.9.9.9", true},
		{"ipv6 loopback", "http://[::1]:8080", true},
		{"public host", "https://api.example.com", false},
		{"lan ip is not loopback", "http://192.168.1.10:8080", false},
		{"empty", "", false},
		{"unparseable", "https://[", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OriginIsLoopback(tc.origin); got != tc.want {
				t.Errorf("OriginIsLoopback(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}

// TestSignedDownloadURLTTL pins the disable-at-non-positive contract: 0 or a
// negative hour count means "signed downloads off" (the signer reports not
// enabled), and a positive count converts to the matching duration.
func TestSignedDownloadURLTTL(t *testing.T) {
	cases := []struct {
		name  string
		hours int
		want  time.Duration
	}{
		{"negative disables", -1, 0},
		{"zero disables", 0, 0},
		{"one hour", 1, time.Hour},
		{"default 168h", 168, 168 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{}
			c.App.Download.SignedURLTTLHours = tc.hours
			if got := c.SignedDownloadURLTTL(); got != tc.want {
				t.Errorf("SignedDownloadURLTTL() with %dh = %v, want %v", tc.hours, got, tc.want)
			}
		})
	}
}
