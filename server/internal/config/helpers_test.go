package config

import (
	"strings"
	"testing"
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
