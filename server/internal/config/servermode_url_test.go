package config

import (
	"net/url"
	"testing"
)

// TestIsUsableWebhookURL pins the canonical Twitch-webhook-acceptance rule
// branch by branch: scheme, host, loopback, and port. Startup validation, API
// validation, and the eventsub runtime guard all defer to it, so a flipped
// condition here would silently let an unreachable callback through (or reject
// a good one). Previously only the loopback and happy paths were exercised
// transitively through direct-mode validation; the scheme, empty-host,
// unparseable, and non-standard-port branches asserted nothing.
func TestIsUsableWebhookURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "empty", raw: "", want: false},
		{name: "public https", raw: "https://replayvod.example/cb", want: true},
		{name: "explicit default port", raw: "https://replayvod.example:443/cb", want: true},
		{name: "http scheme rejected", raw: "http://replayvod.example/cb", want: false},
		{name: "non-default port rejected", raw: "https://replayvod.example:8443/cb", want: false},
		{name: "http default port rejected", raw: "https://replayvod.example:80/cb", want: false},
		{name: "loopback hostname rejected", raw: "https://localhost/cb", want: false},
		{name: "loopback ip rejected", raw: "https://127.0.0.1/cb", want: false},
		{name: "empty host rejected", raw: "https://", want: false},
		{name: "unparseable rejected", raw: "https://[", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUsableWebhookURL(tc.raw); got != tc.want {
				t.Fatalf("IsUsableWebhookURL(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestURLHost pins that a parse failure yields "" rather than a panic or a
// stale host. The error branch was never exercised, and a valid URL must still
// return its host:port verbatim.
func TestURLHost(t *testing.T) {
	if got := URLHost("https://relay.example:443/u/tok"); got != "relay.example:443" {
		t.Fatalf("URLHost(valid) = %q, want host:port", got)
	}
	if got := URLHost("https://["); got != "" {
		t.Fatalf("URLHost(unparseable) = %q, want empty", got)
	}
}

// TestCanonicalURLHost pins port canonicalization exactly: a scheme's own
// default port is stripped, the other scheme's default port is not, and a
// non-default port is always kept. SameURL relies on this to decide whether a
// callback URL changed, which in turn drives RestartRequired.
func TestCanonicalURLHost(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "https://H.Example/p", want: "h.example"},
		{raw: "https://h.example:443/p", want: "h.example"},
		{raw: "https://h.example:8443/p", want: "h.example:8443"},
		{raw: "http://h.example/p", want: "h.example"},
		{raw: "http://h.example:80/p", want: "h.example"},
		{raw: "http://h.example:8080/p", want: "h.example:8080"},
		{raw: "https://h.example:80/p", want: "h.example:80"},
		{raw: "http://h.example:443/p", want: "h.example:443"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("url.Parse(%q) failed: %v", tc.raw, err)
			}
			if got := canonicalURLHost(u); got != tc.want {
				t.Fatalf("canonicalURLHost(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestSameURL covers the comparison's structural decisions: a parse failure on
// either side is not-equal (not a panic), default ports normalize, scheme-
// mismatched default ports do not, and the query string participates.
func TestSameURL(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "https default port equals omitted", a: "https://h.example/cb", b: "https://h.example:443/cb", want: true},
		{name: "http default port equals omitted", a: "http://h.example/cb", b: "http://h.example:80/cb", want: true},
		{name: "https with http default port differs", a: "https://h.example:80/cb", b: "https://h.example/cb", want: false},
		{name: "non-default port differs", a: "https://h.example:8443/cb", b: "https://h.example/cb", want: false},
		{name: "query participates", a: "https://h.example/cb?token=old", b: "https://h.example/cb?token=new", want: false},
		{name: "left unparseable is not equal", a: "https://[", b: "https://h.example/cb", want: false},
		{name: "right unparseable is not equal", a: "https://h.example/cb", b: "https://[", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameURL(tc.a, tc.b); got != tc.want {
				t.Fatalf("SameURL(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestRelayIngestToken pins the /u/<token> shape: each rejection clause (wrong
// segment count, wrong prefix, malformed token) must independently fail, and a
// well-formed path returns its token. Only the happy path was reached before,
// through relay-mode validation with a valid token.
func TestRelayIngestToken(t *testing.T) {
	const token = "AAAAAAAAAAAAAAAA"
	cases := []struct {
		name   string
		path   string
		want   string
		wantOK bool
	}{
		{name: "valid", path: "/u/" + token, want: token, wantOK: true},
		{name: "no leading slash", path: "u/" + token, want: token, wantOK: true},
		{name: "too few segments", path: "/" + token, wantOK: false},
		{name: "too many segments", path: "/u/" + token + "/subscribe", wantOK: false},
		{name: "wrong prefix", path: "/x/" + token, wantOK: false},
		{name: "malformed token", path: "/u/short", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := relayIngestToken(tc.path)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("relayIngestToken(%q) = (%q, %v), want (%q, %v)", tc.path, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestRelaySubscribeToken pins the /u/<token>/subscribe shape, including the
// trailing "subscribe" segment that distinguishes it from the ingest path.
func TestRelaySubscribeToken(t *testing.T) {
	const token = "AAAAAAAAAAAAAAAA"
	cases := []struct {
		name   string
		path   string
		want   string
		wantOK bool
	}{
		{name: "valid", path: "/u/" + token + "/subscribe", want: token, wantOK: true},
		{name: "missing subscribe segment", path: "/u/" + token, wantOK: false},
		{name: "wrong trailing segment", path: "/u/" + token + "/publish", wantOK: false},
		{name: "wrong prefix", path: "/x/" + token + "/subscribe", wantOK: false},
		{name: "malformed token", path: "/u/short/subscribe", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := relaySubscribeToken(tc.path)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("relaySubscribeToken(%q) = (%q, %v), want (%q, %v)", tc.path, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestDeriveRelaySubscribeURL pins that the subscribe URL is a pure function of
// the relay (ingest) URL: same host and token, wss scheme, /subscribe suffix.
// This is what lets the dashboard ask for one relay URL instead of two.
func TestDeriveRelaySubscribeURL(t *testing.T) {
	const token = "AAAAAAAAAAAAAAAA"
	cases := []struct {
		name   string
		ingest string
		want   string
		wantOK bool
	}{
		{name: "valid", ingest: "https://relay.example/u/" + token, want: "wss://relay.example/u/" + token + "/subscribe", wantOK: true},
		{name: "default port host preserved", ingest: "https://relay.example:443/u/" + token, want: "wss://relay.example:443/u/" + token + "/subscribe", wantOK: true},
		{name: "trailing slash tolerated", ingest: "https://relay.example/u/" + token + "/", want: "wss://relay.example/u/" + token + "/subscribe", wantOK: true},
		{name: "http rejected", ingest: "http://relay.example/u/" + token, wantOK: false},
		{name: "missing token path rejected", ingest: "https://relay.example/", wantOK: false},
		{name: "subscribe path rejected", ingest: "https://relay.example/u/" + token + "/subscribe", wantOK: false},
		{name: "unparseable rejected", ingest: "https://[", wantOK: false},
		{name: "empty rejected", ingest: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DeriveRelaySubscribeURL(tc.ingest)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("DeriveRelaySubscribeURL(%q) = (%q, %v), want (%q, %v)", tc.ingest, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestResolveDerivedURLs pins the read/runtime fill: relay subscribe comes from
// the relay URL, the direct callback comes from the public base, both only when
// blank (explicit wins), and other modes are untouched. A relay/direct config
// carrying only the operator-supplied URL must validate after resolution, which
// is what makes the single-field form and the no-field direct mode work.
func TestResolveDerivedURLs(t *testing.T) {
	const (
		token  = "AAAAAAAAAAAAAAAA"
		ingest = "https://relay.example/u/AAAAAAAAAAAAAAAA"
	)

	t.Run("relay fills subscribe from ingest", func(t *testing.T) {
		cfg := ServerModeConfig{Mode: ServerModeRelay, RelayIngestURL: ingest}
		cfg.ResolveDerivedURLs("")
		if want := "wss://relay.example/u/" + token + "/subscribe"; cfg.RelaySubscribeURL != want {
			t.Fatalf("subscribe = %q, want %q", cfg.RelaySubscribeURL, want)
		}
		if err := ValidateServerMode(cfg); err != nil {
			t.Fatalf("relay config with only an ingest URL must validate after resolution: %v", err)
		}
	})

	t.Run("relay keeps explicit subscribe", func(t *testing.T) {
		const explicit = "wss://other.example/u/" + token + "/subscribe"
		cfg := ServerModeConfig{Mode: ServerModeRelay, RelayIngestURL: ingest, RelaySubscribeURL: explicit}
		cfg.ResolveDerivedURLs("")
		if cfg.RelaySubscribeURL != explicit {
			t.Fatalf("subscribe = %q, want explicit %q", cfg.RelaySubscribeURL, explicit)
		}
	})

	t.Run("direct fills callback from public base", func(t *testing.T) {
		cfg := ServerModeConfig{Mode: ServerModeDirect}
		cfg.ResolveDerivedURLs("https://replayvod.example")
		if want := "https://replayvod.example" + webhookCallbackPath; cfg.WebhookCallbackURL != want {
			t.Fatalf("callback = %q, want %q", cfg.WebhookCallbackURL, want)
		}
		if err := ValidateServerMode(cfg); err != nil {
			t.Fatalf("direct config must validate once the callback is derived: %v", err)
		}
	})

	t.Run("direct keeps explicit callback", func(t *testing.T) {
		const explicit = "https://explicit.example/api/v1/webhook/callback"
		cfg := ServerModeConfig{Mode: ServerModeDirect, WebhookCallbackURL: explicit}
		cfg.ResolveDerivedURLs("https://replayvod.example")
		if cfg.WebhookCallbackURL != explicit {
			t.Fatalf("callback = %q, want explicit %q", cfg.WebhookCallbackURL, explicit)
		}
	})

	t.Run("direct without public base stays blank", func(t *testing.T) {
		cfg := ServerModeConfig{Mode: ServerModeDirect}
		cfg.ResolveDerivedURLs("")
		if cfg.WebhookCallbackURL != "" {
			t.Fatalf("callback = %q, want empty when no public base", cfg.WebhookCallbackURL)
		}
	})

	t.Run("poll mode untouched", func(t *testing.T) {
		cfg := ServerModeConfig{Mode: ServerModePoll}
		cfg.ResolveDerivedURLs("https://replayvod.example")
		if cfg.WebhookCallbackURL != "" || cfg.RelaySubscribeURL != "" {
			t.Fatalf("poll mode must not gain URLs: %+v", cfg)
		}
	})
}

// TestValidateRelayMode_MalformedRelayURLReportsAgainstRelayURL pins that a
// malformed relay URL is reported as a relay URL problem. The dashboard sends
// only the relay URL and derives the subscribe URL, so an error about a missing
// subscribe URL would point at an input the owner can't see.
func TestValidateRelayMode_MalformedRelayURLReportsAgainstRelayURL(t *testing.T) {
	const token = "AAAAAAAAAAAAAAAA"
	cases := []struct {
		name   string
		ingest string
		want   string
	}{
		{name: "http scheme", ingest: "http://relay.example/u/" + token, want: "relay URL must be a public HTTPS URL"},
		{name: "no scheme", ingest: "relay.example/u/" + token, want: "relay URL must be a public HTTPS URL"},
		{name: "bad path", ingest: "https://relay.example/wrong", want: "relay URL must use the form https://<host>/u/<token>"},
		{name: "short token", ingest: "https://relay.example/u/short", want: "relay URL must use the form https://<host>/u/<token>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Build as the app does (it derives the subscribe URL), mirroring the
			// dashboard sending only the relay URL.
			cfg := ServerModeConfigFromApp(ServerModeRelay, "", tc.ingest, "", "")
			err := ValidateServerMode(cfg)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ValidateServerMode(%q) error = %v, want %q", tc.ingest, err, tc.want)
			}
		})
	}
}

// TestValidateRelayURLs pins the ingest/subscribe pairing rules directly: an
// empty subscribe URL is allowed (relay subscription is optional), a valid pair
// passes, and every mismatch (host, scheme, token, malformed path) is rejected.
// Previously these were only reached through relay-mode validation with a
// matching pair, so the rejection branches asserted nothing.
func TestValidateRelayURLs(t *testing.T) {
	const (
		ingest    = "https://relay.example/u/AAAAAAAAAAAAAAAA"
		subscribe = "wss://relay.example/u/AAAAAAAAAAAAAAAA/subscribe"
	)
	cases := []struct {
		name      string
		ingest    string
		subscribe string
		wantErr   bool
	}{
		{name: "empty subscribe allowed", ingest: ingest, subscribe: "", wantErr: false},
		{name: "valid pair", ingest: ingest, subscribe: subscribe, wantErr: false},
		{name: "host mismatch", ingest: ingest, subscribe: "wss://other.example/u/AAAAAAAAAAAAAAAA/subscribe", wantErr: true},
		{name: "subscribe not wss", ingest: ingest, subscribe: "https://relay.example/u/AAAAAAAAAAAAAAAA/subscribe", wantErr: true},
		{name: "ingest not https", ingest: "http://relay.example/u/AAAAAAAAAAAAAAAA", subscribe: subscribe, wantErr: true},
		{name: "token mismatch", ingest: ingest, subscribe: "wss://relay.example/u/BBBBBBBBBBBBBBBB/subscribe", wantErr: true},
		{name: "malformed ingest path", ingest: "https://relay.example/wrong", subscribe: subscribe, wantErr: true},
		{name: "unparseable ingest", ingest: "https://[", subscribe: subscribe, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRelayURLs(tc.ingest, tc.subscribe)
			if tc.wantErr != (err != nil) {
				t.Fatalf("ValidateRelayURLs(%q, %q) err = %v, wantErr %v", tc.ingest, tc.subscribe, err, tc.wantErr)
			}
		})
	}
}
