package config

import "testing"

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
