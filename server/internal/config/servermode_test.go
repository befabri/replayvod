package config

import (
	"strings"
	"testing"
)

func TestServerModeConfigProcessesWebhookNotifications(t *testing.T) {
	cases := []struct {
		name string
		mode string
		want bool
	}{
		{name: "unset", mode: "", want: false},
		{name: "off", mode: ServerModeOff, want: false},
		{name: "poll", mode: ServerModePoll, want: false},
		{name: "direct", mode: ServerModeDirect, want: true},
		{name: "relay", mode: ServerModeRelay, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (ServerModeConfig{Mode: tc.mode}).ProcessesWebhookNotifications()
			if got != tc.want {
				t.Fatalf("ProcessesWebhookNotifications() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestValidateServerMode_RejectsURLFieldsForeignToMode pins that every
// mode rejects URL fields that don't belong to it, the same way the
// unset and direct branches already do. Without this a half-set config such as
// SERVER_MODE=off plus WEBHOOK_CALLBACK_URL=https://... is silently
// accepted and the callback URL is quietly dropped, hiding the misconfig.
func TestValidateServerMode_RejectsURLFieldsForeignToMode(t *testing.T) {
	const (
		webhookURL   = "https://replayvod.example/api/v1/webhook/callback"
		ingestURL    = "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"
		subscribeURL = "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe"
		localURL     = "http://127.0.0.1:8080/api/v1/webhook/callback"
	)
	cases := []struct {
		name string
		cfg  ServerModeConfig
	}{
		{
			name: "off with webhook callback",
			cfg:  ServerModeConfig{Mode: ServerModeOff, WebhookCallbackURL: webhookURL},
		},
		{
			name: "poll with webhook callback",
			cfg:  ServerModeConfig{Mode: ServerModePoll, WebhookCallbackURL: webhookURL},
		},
		{
			name: "relay with webhook callback",
			cfg: ServerModeConfig{
				Mode:               ServerModeRelay,
				WebhookCallbackURL: webhookURL,
				RelayIngestURL:     ingestURL,
				RelaySubscribeURL:  subscribeURL,
			},
		},
		{
			name: "direct with relay local callback",
			cfg: ServerModeConfig{
				Mode:                  ServerModeDirect,
				WebhookCallbackURL:    webhookURL,
				RelayLocalCallbackURL: localURL,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateServerMode(tc.cfg); err == nil {
				t.Fatalf("ValidateServerMode(%s) = nil, want error for foreign URL field", tc.name)
			}
		})
	}
}

// TestValidateServerMode_AcceptsCleanConfigs guards against the rejection
// above being too broad: a config carrying only the fields its mode
// owns must still validate.
func TestValidateServerMode_AcceptsCleanConfigs(t *testing.T) {
	cases := []struct {
		name string
		cfg  ServerModeConfig
	}{
		{name: "off bare", cfg: ServerModeConfig{Mode: ServerModeOff}},
		{name: "poll bare", cfg: ServerModeConfig{Mode: ServerModePoll}},
		{
			name: "direct webhook only",
			cfg: ServerModeConfig{
				Mode:               ServerModeDirect,
				WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback",
			},
		},
		{
			name: "relay urls only",
			cfg: ServerModeConfig{
				Mode:              ServerModeRelay,
				RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
				RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateServerMode(tc.cfg); err != nil {
				t.Fatalf("ValidateServerMode(%s) = %v, want nil", tc.name, err)
			}
		})
	}
}

func TestValidateServerModeHMACSecret(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServerModeConfig
		secret  string
		wantErr bool
	}{
		{
			name:   "unset allows empty secret",
			cfg:    ServerModeConfig{},
			secret: "",
		},
		{
			name:   "off allows empty secret",
			cfg:    ServerModeConfig{Mode: ServerModeOff},
			secret: "",
		},
		{
			name:    "direct requires secret",
			cfg:     ServerModeConfig{Mode: ServerModeDirect},
			secret:  "",
			wantErr: true,
		},
		{
			name:    "relay rejects short secret",
			cfg:     ServerModeConfig{Mode: ServerModeRelay},
			secret:  "too-short",
			wantErr: true,
		},
		{
			name:    "direct rejects non ascii secret",
			cfg:     ServerModeConfig{Mode: ServerModeDirect},
			secret:  "012345678é",
			wantErr: true,
		},
		{
			name:   "direct accepts Twitch-sized ascii secret",
			cfg:    ServerModeConfig{Mode: ServerModeDirect},
			secret: "0123456789abcdef",
		},
		{
			name:   "direct accepts secret at min length",
			cfg:    ServerModeConfig{Mode: ServerModeDirect},
			secret: "0123456789", // exactly 10 bytes
		},
		{
			name:   "direct accepts secret at max length",
			cfg:    ServerModeConfig{Mode: ServerModeDirect},
			secret: strings.Repeat("a", 100),
		},
		{
			name:    "direct rejects secret over max length",
			cfg:     ServerModeConfig{Mode: ServerModeDirect},
			secret:  strings.Repeat("a", 101),
			wantErr: true,
		},
		{
			name:   "direct accepts DEL as the highest ASCII rune",
			cfg:    ServerModeConfig{Mode: ServerModeDirect},
			secret: "012345678\x7f", // 10 bytes, all <= 127
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateServerModeHMACSecret(tc.cfg, tc.secret)
			if tc.wantErr && err == nil {
				t.Fatal("ValidateServerModeHMACSecret() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateServerModeHMACSecret() = %v, want nil", err)
			}
		})
	}
}

func TestSameURLIncludesQueryAndNormalizesDefaultPort(t *testing.T) {
	if !SameURL("https://replayvod.example/api/v1/webhook/callback", "https://replayvod.example:443/api/v1/webhook/callback") {
		t.Fatal("SameURL() = false for equivalent HTTPS default port")
	}
	if SameURL("https://replayvod.example/api/v1/webhook/callback?token=old", "https://replayvod.example/api/v1/webhook/callback?token=new") {
		t.Fatal("SameURL() = true for different query strings")
	}
}

// TestServerModeConfigDerivedState pins that SetupRequired and EnvManaged are
// derived from Mode and Source rather than stored as separate booleans that
// could disagree.
func TestServerModeConfigDerivedState(t *testing.T) {
	cases := []struct {
		name           string
		cfg            ServerModeConfig
		wantSetup      bool
		wantEnvManaged bool
	}{
		{name: "unset", cfg: ServerModeConfig{Source: ServerModeConfigSourceUnset}, wantSetup: true},
		{name: "env relay", cfg: ServerModeConfig{Source: ServerModeConfigSourceEnv, Mode: ServerModeRelay}, wantEnvManaged: true},
		{name: "app off", cfg: ServerModeConfig{Source: ServerModeConfigSourceApp, Mode: ServerModeOff}},
		{name: "whitespace mode is setup", cfg: ServerModeConfig{Source: ServerModeConfigSourceApp, Mode: "  "}, wantSetup: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.SetupRequired(); got != tc.wantSetup {
				t.Fatalf("SetupRequired() = %v, want %v", got, tc.wantSetup)
			}
			if got := tc.cfg.EnvManaged(); got != tc.wantEnvManaged {
				t.Fatalf("EnvManaged() = %v, want %v", got, tc.wantEnvManaged)
			}
		})
	}
}

// TestNormalizeForcesUnsetSourceWhenModeEmpty pins the single-source-of-
// truth invariant: an empty mode is the unset state no matter what Source a
// caller declared.
func TestNormalizeForcesUnsetSourceWhenModeEmpty(t *testing.T) {
	cfg := ServerModeConfig{Source: ServerModeConfigSourceApp, Mode: "   "}
	cfg.Normalize()
	if cfg.Source != ServerModeConfigSourceUnset {
		t.Fatalf("Source = %q, want %q for empty mode", cfg.Source, ServerModeConfigSourceUnset)
	}
}

// TestServerModeConfigFromApp_ClearsURLsForeignToMode pins that app config is
// canonical at construction: the URL fields a mode does not use are
// cleared, so storage, runtime, and the API response never carry stale URLs.
func TestServerModeConfigFromApp_ClearsURLsForeignToMode(t *testing.T) {
	const (
		webhookURL   = "https://replayvod.example/api/v1/webhook/callback"
		ingestURL    = "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"
		subscribeURL = "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe"
		localURL     = "http://127.0.0.1:8080/api/v1/webhook/callback"
	)

	direct := ServerModeConfigFromApp(ServerModeDirect, webhookURL, ingestURL, subscribeURL, localURL)
	if direct.WebhookCallbackURL != webhookURL {
		t.Fatalf("direct WebhookCallbackURL = %q, want kept", direct.WebhookCallbackURL)
	}
	if direct.RelayIngestURL != "" || direct.RelaySubscribeURL != "" || direct.RelayLocalCallbackURL != "" {
		t.Fatalf("direct retained relay URLs: %#v", direct)
	}

	relay := ServerModeConfigFromApp(ServerModeRelay, webhookURL, ingestURL, subscribeURL, localURL)
	if relay.WebhookCallbackURL != "" {
		t.Fatalf("relay retained webhook URL: %q", relay.WebhookCallbackURL)
	}
	if relay.RelayIngestURL != ingestURL || relay.RelaySubscribeURL != subscribeURL {
		t.Fatalf("relay dropped its own URLs: %#v", relay)
	}

	off := ServerModeConfigFromApp(ServerModeOff, webhookURL, ingestURL, subscribeURL, localURL)
	if off.WebhookCallbackURL != "" || off.RelayIngestURL != "" || off.RelaySubscribeURL != "" || off.RelayLocalCallbackURL != "" {
		t.Fatalf("off retained URLs: %#v", off)
	}

	poll := ServerModeConfigFromApp(ServerModePoll, webhookURL, ingestURL, subscribeURL, localURL)
	if poll.WebhookCallbackURL != "" || poll.RelayIngestURL != "" || poll.RelaySubscribeURL != "" || poll.RelayLocalCallbackURL != "" {
		t.Fatalf("poll retained URLs: %#v", poll)
	}

	empty := ServerModeConfigFromApp("", "", "", "", "")
	if empty.Source != ServerModeConfigSourceUnset || !empty.SetupRequired() {
		t.Fatalf("empty mode = %#v, want unset/setup-required", empty)
	}
}

// TestValidateServerModeMessagesAreFieldNeutral pins that validation errors,
// which surface verbatim to dashboard users, never name environment variables.
// The same function validates env config at boot and owner config from the
// dashboard, so env-var-flavored text would be wrong for the dashboard half.
func TestValidateServerModeMessagesAreFieldNeutral(t *testing.T) {
	envTokens := []string{
		"SERVER_MODE", "WEBHOOK_CALLBACK_URL", "RELAY_INGEST_URL",
		"RELAY_SUBSCRIBE_URL", "RELAY_LOCAL_CALLBACK_URL",
	}
	cases := []ServerModeConfig{
		{Mode: ServerModeDirect}, // missing usable callback
		{Mode: ServerModeDirect, WebhookCallbackURL: "https://localhost/cb"},
		{Mode: ServerModeRelay}, // missing relay URLs
		{Mode: ServerModeOff, WebhookCallbackURL: "https://x.example/cb"},  // foreign URL
		{Mode: ServerModePoll, WebhookCallbackURL: "https://x.example/cb"}, // foreign URL
		{Mode: "bogus"}, // unknown mode
		{WebhookCallbackURL: "https://x.example/cb"}, // URL without mode
	}
	for _, cfg := range cases {
		err := ValidateServerMode(cfg)
		if err == nil {
			t.Fatalf("ValidateServerMode(%+v) = nil, want error", cfg)
		}
		for _, tok := range envTokens {
			if strings.Contains(err.Error(), tok) {
				t.Fatalf("error %q names env var %q; messages must be field-neutral", err.Error(), tok)
			}
		}
	}
}

// TestServerModeCapabilityPredicatesPerMode pins each mode-routing predicate to
// the exact set of modes it must be true for. A copy-paste returning true for
// the wrong mode (e.g. PollsHelix in direct mode) would silently wire the wrong
// subsystem at boot, and nothing else asserts these in isolation.
func TestServerModeCapabilityPredicatesPerMode(t *testing.T) {
	type caps struct {
		creates, processesWebhook, usesRelay, pollsHelix, titlesPoll, titlesWebhook bool
	}
	cases := map[string]caps{
		"":               {},
		ServerModeOff:    {},
		ServerModePoll:   {pollsHelix: true, titlesPoll: true},
		ServerModeDirect: {creates: true, processesWebhook: true, titlesWebhook: true},
		ServerModeRelay:  {creates: true, processesWebhook: true, usesRelay: true, titlesWebhook: true},
	}
	for mode, want := range cases {
		t.Run("mode="+mode, func(t *testing.T) {
			c := ServerModeConfig{Mode: mode}
			if got := c.CreatesTwitchSubscriptions(); got != want.creates {
				t.Errorf("CreatesTwitchSubscriptions = %v, want %v", got, want.creates)
			}
			if got := c.ProcessesWebhookNotifications(); got != want.processesWebhook {
				t.Errorf("ProcessesWebhookNotifications = %v, want %v", got, want.processesWebhook)
			}
			if got := c.UsesRelayAgent(); got != want.usesRelay {
				t.Errorf("UsesRelayAgent = %v, want %v", got, want.usesRelay)
			}
			if got := c.PollsHelix(); got != want.pollsHelix {
				t.Errorf("PollsHelix = %v, want %v", got, want.pollsHelix)
			}
			if got := c.TracksTitlesViaPoll(); got != want.titlesPoll {
				t.Errorf("TracksTitlesViaPoll = %v, want %v", got, want.titlesPoll)
			}
			if got := c.TracksTitlesViaWebhook(); got != want.titlesWebhook {
				t.Errorf("TracksTitlesViaWebhook = %v, want %v", got, want.titlesWebhook)
			}
		})
	}
}

// TestValidateRelayLocalCallbackURL covers the loopback-only SSRF guard for the
// relay agent's replay target: every rejection path plus the loopback happy
// path. Only reached indirectly via relay-mode validation otherwise.
func TestValidateRelayLocalCallbackURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "empty is allowed (uses default)", raw: ""},
		{name: "loopback ip", raw: "http://127.0.0.1:8080/api/v1/webhook/callback"},
		{name: "localhost", raw: "https://localhost/api/v1/webhook/callback"},
		{name: "ipv6 loopback", raw: "http://[::1]:8080/api/v1/webhook/callback"},
		{name: "non-loopback host", raw: "http://example.com/api/v1/webhook/callback", wantErr: true},
		{name: "private but non-loopback ip", raw: "https://10.0.0.5/api/v1/webhook/callback", wantErr: true},
		{name: "wrong path", raw: "http://127.0.0.1:8080/wrong", wantErr: true},
		{name: "non-http scheme", raw: "ftp://127.0.0.1/api/v1/webhook/callback", wantErr: true},
		{name: "unparseable", raw: "http://[", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRelayLocalCallbackURL(tc.raw)
			if tc.wantErr != (err != nil) {
				t.Fatalf("validateRelayLocalCallbackURL(%q) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
		})
	}
}

// TestServerModeConfigFromEnv pins the un-cleared contract: env config keeps URL
// fields foreign to its mode (so ValidateServerMode can reject a half-set env
// instead of silently dropping fields), unlike the app constructor which clears
// them. An empty mode collapses to the unset source.
func TestServerModeConfigFromEnv(t *testing.T) {
	notConfigured := ServerModeConfigFromEnv(Environment{ServerModeEnvConfigured: false})
	if notConfigured.Source != ServerModeConfigSourceUnset {
		t.Fatalf("unconfigured env Source = %q, want unset", notConfigured.Source)
	}

	// A direct env config that also carries a relay URL must survive un-cleared.
	direct := ServerModeConfigFromEnv(Environment{
		ServerModeEnvConfigured: true,
		ServerMode:              ServerModeDirect,
		WebhookCallbackURL:      "https://replayvod.example/api/v1/webhook/callback",
		RelayIngestURL:          "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
	})
	if direct.Source != ServerModeConfigSourceEnv {
		t.Fatalf("env Source = %q, want env", direct.Source)
	}
	if direct.RelayIngestURL == "" {
		t.Fatal("env config cleared the foreign relay URL; it must survive so validation can reject it")
	}
	if err := ValidateServerMode(direct); err == nil {
		t.Fatal("ValidateServerMode(direct env with relay URL) = nil; the surviving URL must be rejected")
	}

	// Empty mode collapses to unset regardless of the env-configured flag.
	empty := ServerModeConfigFromEnv(Environment{ServerModeEnvConfigured: true, ServerMode: ""})
	if empty.Source != ServerModeConfigSourceUnset || !empty.SetupRequired() {
		t.Fatalf("empty-mode env = %#v, want unset/setup-required", empty)
	}
}

// TestRuntimeEqualComparesDeliveryURLsNotSource pins that RuntimeEqual ignores
// Source (env vs app) but treats a URL-only difference as not-equal, which is
// what drives RestartRequired when only a callback URL changes.
func TestRuntimeEqualComparesDeliveryURLsNotSource(t *testing.T) {
	base := ServerModeConfig{
		Source:             ServerModeConfigSourceApp,
		Mode:               ServerModeDirect,
		WebhookCallbackURL: "https://a.example/api/v1/webhook/callback",
	}
	sameRuntimeDifferentSource := base
	sameRuntimeDifferentSource.Source = ServerModeConfigSourceEnv
	if !base.RuntimeEqual(sameRuntimeDifferentSource) {
		t.Fatal("RuntimeEqual must ignore Source")
	}

	urlChanged := base
	urlChanged.WebhookCallbackURL = "https://b.example/api/v1/webhook/callback"
	if base.RuntimeEqual(urlChanged) {
		t.Fatal("RuntimeEqual must treat a callback-URL-only change as not equal")
	}
}

// TestRelayLocalCallbackURLOrDefault pins the default loopback formatting when no
// explicit local callback is configured, and that an explicit one wins.
func TestRelayLocalCallbackURLOrDefault(t *testing.T) {
	none := ServerModeConfig{Mode: ServerModeRelay}
	if got := none.RelayLocalCallbackURLOrDefault(9000); got != "http://127.0.0.1:9000/api/v1/webhook/callback" {
		t.Fatalf("default local callback = %q, want loopback on the given port", got)
	}
	explicit := ServerModeConfig{Mode: ServerModeRelay, RelayLocalCallbackURL: "http://localhost:1234/api/v1/webhook/callback"}
	if got := explicit.RelayLocalCallbackURLOrDefault(9000); got != "http://localhost:1234/api/v1/webhook/callback" {
		t.Fatalf("explicit local callback = %q, want it kept over the default", got)
	}
}

// TestValidateServerMode_RejectsEachForeignURLFieldAlone pins that, for the
// no-URL modes, each of the four URL fields is rejected on its own. The
// existing foreign-URL test sets one field per mode, which leaves the "any URL
// set" boolean chain free to collapse to a single field without any test
// noticing. Setting one field at a time forces every term of the chain to
// matter.
func TestValidateServerMode_RejectsEachForeignURLFieldAlone(t *testing.T) {
	const (
		webhookURL   = "https://replayvod.example/api/v1/webhook/callback"
		ingestURL    = "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"
		subscribeURL = "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe"
		localURL     = "http://127.0.0.1:8080/api/v1/webhook/callback"
	)
	fields := []struct {
		name string
		set  func(*ServerModeConfig)
	}{
		{name: "webhook", set: func(c *ServerModeConfig) { c.WebhookCallbackURL = webhookURL }},
		{name: "ingest", set: func(c *ServerModeConfig) { c.RelayIngestURL = ingestURL }},
		{name: "subscribe", set: func(c *ServerModeConfig) { c.RelaySubscribeURL = subscribeURL }},
		{name: "local", set: func(c *ServerModeConfig) { c.RelayLocalCallbackURL = localURL }},
	}
	for _, mode := range []string{"", ServerModeOff, ServerModePoll} {
		for _, f := range fields {
			t.Run("mode="+mode+"/"+f.name, func(t *testing.T) {
				cfg := ServerModeConfig{Mode: mode}
				f.set(&cfg)
				if err := ValidateServerMode(cfg); err == nil {
					t.Fatalf("ValidateServerMode(mode=%q, %s set) = nil, want error", mode, f.name)
				}
			})
		}
	}
}

// TestValidateServerMode_RelayPropagatesPairError pins that relay mode surfaces
// an error from the ingest/subscribe pairing check rather than swallowing it.
// Without this, the err-propagation branch could be inverted and a mismatched
// relay pair would validate.
func TestValidateServerMode_RelayPropagatesPairError(t *testing.T) {
	cfg := ServerModeConfig{
		Mode:              ServerModeRelay,
		RelayIngestURL:    "https://relay-a.example/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay-b.example/u/AAAAAAAAAAAAAAAA/subscribe", // host mismatch
	}
	if err := ValidateServerMode(cfg); err == nil {
		t.Fatal("ValidateServerMode(relay with mismatched ingest/subscribe host) = nil, want error")
	}
}

// TestValidateServerModeRuntimeConfig pins that the runtime wrapper runs both
// legs: the mode/URL validation and the HMAC-secret check. An invalid mode must
// fail even with a good secret, and a good config must fail on a bad secret.
func TestValidateServerModeRuntimeConfig(t *testing.T) {
	const (
		validSecret   = "0123456789abcdef"
		directWebhook = "https://replayvod.example/api/v1/webhook/callback"
	)
	cases := []struct {
		name    string
		cfg     ServerModeConfig
		secret  string
		wantErr bool
	}{
		{
			name:    "invalid mode fails despite valid secret",
			cfg:     ServerModeConfig{Mode: "bogus"},
			secret:  validSecret,
			wantErr: true,
		},
		{
			name:   "valid direct config with valid secret",
			cfg:    ServerModeConfig{Mode: ServerModeDirect, WebhookCallbackURL: directWebhook},
			secret: validSecret,
		},
		{
			name:    "valid direct config with short secret fails",
			cfg:     ServerModeConfig{Mode: ServerModeDirect, WebhookCallbackURL: directWebhook},
			secret:  "short",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateServerModeRuntimeConfig(tc.cfg, tc.secret)
			if tc.wantErr != (err != nil) {
				t.Fatalf("ValidateServerModeRuntimeConfig(%s) err = %v, wantErr %v", tc.name, err, tc.wantErr)
			}
		})
	}
}
