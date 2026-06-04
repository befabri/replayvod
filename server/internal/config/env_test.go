package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	envparse "github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

func TestMain(m *testing.M) {
	clearURLTestEnv()
	os.Exit(m.Run())
}

func clearURLTestEnv() {
	for _, key := range []string{"CALLBACK_URL", "FRONTEND_URL", "PUBLIC_BASE_URL"} {
		if err := os.Setenv(key, ""); err != nil {
			panic(err)
		}
	}
}

func TestValidateDotenvNoDuplicateKeysRejectsDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("RELAY_SUBSCRIBE_URL=wss://one\n# comment\nRELAY_SUBSCRIBE_URL=wss://two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateDotenvNoDuplicateKeys(path); err == nil {
		t.Fatal("validateDotenvNoDuplicateKeys(duplicate) = nil, want error")
	}
}

// TestValidateDotenvNoDuplicateKeysAllowsMissingFile pins that a missing .env is
// not an error: the check is best-effort and only fires when a file exists.
func TestValidateDotenvNoDuplicateKeysAllowsMissingFile(t *testing.T) {
	if err := validateDotenvNoDuplicateKeys(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
		t.Fatalf("validateDotenvNoDuplicateKeys(missing) = %v, want nil", err)
	}
}

func TestValidateDotenvNoDuplicateKeysAcceptsUniqueExportedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("export SERVER_MODE=relay\nRELAY_SUBSCRIBE_URL=wss://relay.example/u/token/subscribe\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateDotenvNoDuplicateKeys(path); err != nil {
		t.Fatalf("validateDotenvNoDuplicateKeys(unique) = %v, want nil", err)
	}
}

// TestValidateDotenvNoDuplicateKeysIgnoresMultilineQuotedValue pins that the
// interior of a multi-line quoted value is not mistaken for a key. godotenv
// folds the physical lines of a quoted value into one assignment, so a line
// inside the value that happens to look like KEY=... must not trip the
// duplicate check (which would refuse to boot on a perfectly valid .env).
func TestValidateDotenvNoDuplicateKeysIgnoresMultilineQuotedValue(t *testing.T) {
	cases := map[string]string{
		"double quoted": "PASSWORD=\"first\nPASSWORD=second\nthird\"\nRELAY_SUBSCRIBE_URL=wss://relay.example/u/token/subscribe\n",
		"single quoted": "PASSWORD='first\nPASSWORD=second\nthird'\nRELAY_SUBSCRIBE_URL=wss://relay.example/u/token/subscribe\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			// The file is valid godotenv with a single PASSWORD key; the
			// duplicate check must agree rather than reject it.
			parsed, err := godotenv.Read(path)
			if err != nil {
				t.Fatalf("godotenv.Read = %v, want nil", err)
			}
			if _, dup := parsed["second"]; dup {
				t.Fatal("godotenv parsed an interior line as its own key; test premise is wrong")
			}

			if err := validateDotenvNoDuplicateKeys(path); err != nil {
				t.Fatalf("validateDotenvNoDuplicateKeys(multiline value) = %v, want nil", err)
			}
		})
	}
}

// TestValidateDotenvNoDuplicateKeysDetectsDuplicateAfterBOM pins that a UTF-8
// BOM on the first line does not get folded into the first key, which would let
// a real duplicate of that key slip past the check.
func TestValidateDotenvNoDuplicateKeysDetectsDuplicateAfterBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "\ufeffSERVER_MODE=relay\nSERVER_MODE=off\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateDotenvNoDuplicateKeys(path); err == nil {
		t.Fatal("validateDotenvNoDuplicateKeys(BOM + duplicate) = nil, want error")
	}
}

// TestValidateDotenvNoDuplicateKeysDetectsTabExportedDuplicate pins that an
// export prefix separated by a tab is recognized, so the exported key is not
// invisible to the duplicate check.
func TestValidateDotenvNoDuplicateKeysDetectsTabExportedDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "export\tRELAY_SUBSCRIBE_URL=wss://a\nRELAY_SUBSCRIBE_URL=wss://b\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateDotenvNoDuplicateKeys(path); err == nil {
		t.Fatal("validateDotenvNoDuplicateKeys(tab export + duplicate) = nil, want error")
	}
}

// TestValidateDotenvNoDuplicateKeysRejectsSingleLineQuotedDuplicate guards
// against the multiline handling being too eager: a quoted value that closes on
// its own line must not swallow the following lines, so a real duplicate is
// still caught.
func TestValidateDotenvNoDuplicateKeysRejectsSingleLineQuotedDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "FOO=\"bar\"\nFOO=\"baz\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateDotenvNoDuplicateKeys(path); err == nil {
		t.Fatal("validateDotenvNoDuplicateKeys(single-line quoted duplicate) = nil, want error")
	}
}

func TestValidateEnvironmentNormalizesServerMode(t *testing.T) {
	env := &Environment{SessionSecret: "0123456789abcdef0123456789abcdef", ServerMode: " RELAY "}

	if err := validateEnvironment(env); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if env.ServerMode != ServerModeRelay {
		t.Fatalf("ServerMode = %q, want %q", env.ServerMode, ServerModeRelay)
	}
	if !env.ServerModeEnvConfigured {
		t.Fatal("ServerModeEnvConfigured = false, want true")
	}
}

func TestValidateEnvironmentLeavesEmptyServerModeAppManaged(t *testing.T) {
	env := &Environment{SessionSecret: "0123456789abcdef0123456789abcdef"}

	if err := validateEnvironment(env); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if env.ServerMode != "" {
		t.Fatalf("ServerMode = %q, want empty", env.ServerMode)
	}
	if env.ServerModeEnvConfigured {
		t.Fatal("ServerModeEnvConfigured = true, want false")
	}
	if env.CallbackURL != "http://localhost:8080/api/v1/auth/twitch/callback" {
		t.Fatalf("CallbackURL = %q, want local default", env.CallbackURL)
	}
	if env.FrontendURL != "http://localhost:3000" {
		t.Fatalf("FrontendURL = %q, want local dev default", env.FrontendURL)
	}
}

func TestEnvExampleKeepsPublicBaseURLBlankForLocalDev(t *testing.T) {
	values, err := godotenv.Read(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if got, ok := values["PUBLIC_BASE_URL"]; !ok || got != "" {
		t.Fatalf(".env.example PUBLIC_BASE_URL = %q (present %v), want blank so task dev keeps FRONTEND_URL on :3000", got, ok)
	}
}

func TestEnvExampleBlankValuesParseEmpty(t *testing.T) {
	values, err := godotenv.Read(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	for _, key := range []string{"SESSION_SECRET", "WHITELISTED_USER_IDS", "OWNER_TWITCH_ID", "DASHBOARD_DIR"} {
		if got := values[key]; got != "" {
			t.Fatalf(".env.example %s = %q, want empty; keep comments on their own lines so godotenv does not parse them as values", key, got)
		}
	}
}

func TestValidateEnvironmentDerivesURLsFromPublicBaseURL(t *testing.T) {
	env := &Environment{
		SessionSecret: "0123456789abcdef0123456789abcdef",
		PublicBaseURL: " https://replayvod.example/ ",
	}

	if err := validateEnvironment(env); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if env.PublicBaseURL != "https://replayvod.example" {
		t.Fatalf("PublicBaseURL = %q, want normalized origin", env.PublicBaseURL)
	}
	if env.CallbackURL != "https://replayvod.example/api/v1/auth/twitch/callback" {
		t.Fatalf("CallbackURL = %q, want derived callback", env.CallbackURL)
	}
	if env.FrontendURL != "https://replayvod.example" {
		t.Fatalf("FrontendURL = %q, want derived frontend", env.FrontendURL)
	}
}

func TestValidateEnvironmentRejectsPublicBaseURLPathClearly(t *testing.T) {
	env := &Environment{
		SessionSecret: "0123456789abcdef0123456789abcdef",
		PublicBaseURL: "https://replayvod.example/replayvod",
	}

	err := validateEnvironment(env)
	if err == nil {
		t.Fatal("validateEnvironment(PUBLIC_BASE_URL with path) = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "PUBLIC_BASE_URL") || !strings.Contains(msg, "must not include a path") {
		t.Fatalf("error = %q, want clear PUBLIC_BASE_URL path error", msg)
	}
	if strings.Contains(msg, "CALLBACK_URL") {
		t.Fatalf("error = %q, should not mention derived CALLBACK_URL", msg)
	}
}

func TestValidateEnvironmentPublicBaseURLOverridesPrefilledURLFields(t *testing.T) {
	env := &Environment{
		SessionSecret: "0123456789abcdef0123456789abcdef",
		PublicBaseURL: "https://replayvod.example",
		CallbackURL:   "http://localhost:8080/api/v1/auth/twitch/callback",
		FrontendURL:   "http://localhost:3000",
	}

	if err := validateEnvironment(env); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if env.CallbackURL != "https://replayvod.example/api/v1/auth/twitch/callback" {
		t.Fatalf("CallbackURL = %q, want public-base derived callback", env.CallbackURL)
	}
	if env.FrontendURL != "https://replayvod.example" {
		t.Fatalf("FrontendURL = %q, want public-base derived frontend", env.FrontendURL)
	}
}

func TestValidateEnvironmentPublicBaseURLWinsOverProgrammaticURLFields(t *testing.T) {
	env := &Environment{
		SessionSecret: "0123456789abcdef0123456789abcdef",
		PublicBaseURL: "https://videos.example",
		CallbackURL:   "https://auth.example/api/v1/auth/twitch/callback",
		FrontendURL:   "https://dashboard.example",
	}

	if err := validateEnvironment(env); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if env.CallbackURL != "https://videos.example/api/v1/auth/twitch/callback" {
		t.Fatalf("CallbackURL = %q, want public-base derived callback", env.CallbackURL)
	}
	if env.FrontendURL != "https://videos.example" {
		t.Fatalf("FrontendURL = %q, want public-base derived frontend", env.FrontendURL)
	}
}

func TestValidateEnvironmentRejectsPathOnlyCallbackURL(t *testing.T) {
	env := &Environment{
		SessionSecret: "0123456789abcdef0123456789abcdef",
		CallbackURL:   "/api/v1/auth/twitch/callback",
	}

	if err := validateEnvironment(env); err == nil {
		t.Fatal("validateEnvironment(path-only CALLBACK_URL) = nil, want error")
	}
}

func TestValidateEnvironmentPublicBaseURLFixesPathOnlyCallbackURL(t *testing.T) {
	env := &Environment{
		SessionSecret: "0123456789abcdef0123456789abcdef",
		PublicBaseURL: "https://replayvod.example",
		CallbackURL:   "/api/v1/auth/twitch/callback",
	}

	if err := validateEnvironment(env); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil: PUBLIC_BASE_URL should repair old compose interpolation", err)
	}
	if env.CallbackURL != "https://replayvod.example/api/v1/auth/twitch/callback" {
		t.Fatalf("CallbackURL = %q, want public-base derived callback", env.CallbackURL)
	}
}

func TestValidateEnvironmentValidatesBootstrapUserIDs(t *testing.T) {
	t.Run("valid owner and whitelist", func(t *testing.T) {
		env := &Environment{
			SessionSecret:      "0123456789abcdef0123456789abcdef",
			OwnerTwitchID:      "126462569",
			WhitelistedUserIDs: "123, 456",
		}
		if err := validateEnvironment(env); err != nil {
			t.Fatalf("validateEnvironment = %v, want nil", err)
		}
	})

	t.Run("owner must be numeric id", func(t *testing.T) {
		env := &Environment{
			SessionSecret: "0123456789abcdef0123456789abcdef",
			OwnerTwitchID: "piim",
		}
		err := validateEnvironment(env)
		if err == nil {
			t.Fatal("validateEnvironment(owner login) = nil, want error")
		}
		if !strings.Contains(err.Error(), "OWNER_TWITCH_ID") {
			t.Fatalf("error = %q, want OWNER_TWITCH_ID", err)
		}
	})

	t.Run("whitelist must be numeric ids", func(t *testing.T) {
		env := &Environment{
			SessionSecret:      "0123456789abcdef0123456789abcdef",
			WhitelistedUserIDs: "# Comma-separated Twitch user IDs",
		}
		err := validateEnvironment(env)
		if err == nil {
			t.Fatal("validateEnvironment(comment whitelist) = nil, want error")
		}
		if !strings.Contains(err.Error(), "WHITELISTED_USER_IDS") {
			t.Fatalf("error = %q, want WHITELISTED_USER_IDS", err)
		}
	})
}

func TestValidateEnvironmentRejectsEventSubURLsWithoutMode(t *testing.T) {
	cases := map[string]Environment{
		"webhook callback":     {SessionSecret: "0123456789abcdef0123456789abcdef", WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback"},
		"relay ingest":         {SessionSecret: "0123456789abcdef0123456789abcdef", RelayIngestURL: "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"},
		"relay subscribe":      {SessionSecret: "0123456789abcdef0123456789abcdef", RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe"},
		"relay local callback": {SessionSecret: "0123456789abcdef0123456789abcdef", RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateEnvironment(&env); err == nil {
				t.Fatalf("validateEnvironment(%s without SERVER_MODE) = nil, want error", name)
			}
			if env.ServerModeEnvConfigured {
				t.Fatal("ServerModeEnvConfigured = true, want false")
			}
		})
	}
}

func TestEnvironmentParseIgnoresDerivedServerModeConfiguredFlag(t *testing.T) {
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("SERVER_MODE_ENV_CONFIGURED", "true")
	t.Setenv("SERVER_MODE", "")

	var parsed Environment
	if err := envparse.Parse(&parsed); err != nil {
		t.Fatalf("env.Parse = %v, want nil", err)
	}
	if err := validateEnvironment(&parsed); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if parsed.ServerModeEnvConfigured {
		t.Fatal("ServerModeEnvConfigured = true, want derived false")
	}
}

func TestEnvironmentParseIgnoresLegacyURLVariables(t *testing.T) {
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("PUBLIC_BASE_URL", "https://replayvod.example")
	t.Setenv("CALLBACK_URL", "https://legacy.example/api/v1/auth/twitch/callback")
	t.Setenv("FRONTEND_URL", "https://legacy.example")

	var parsed Environment
	if err := envparse.Parse(&parsed); err != nil {
		t.Fatalf("env.Parse = %v, want nil", err)
	}
	if err := validateEnvironment(&parsed); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if parsed.PublicBaseURL != "https://replayvod.example" {
		t.Fatalf("PublicBaseURL = %q, want parsed public base", parsed.PublicBaseURL)
	}
	if parsed.CallbackURL != "https://replayvod.example/api/v1/auth/twitch/callback" {
		t.Fatalf("CallbackURL = %q, want derived from PUBLIC_BASE_URL", parsed.CallbackURL)
	}
	if parsed.FrontendURL != "https://replayvod.example" {
		t.Fatalf("FrontendURL = %q, want derived from PUBLIC_BASE_URL", parsed.FrontendURL)
	}
}

func TestValidateEnvironmentRejectsUnknownServerMode(t *testing.T) {
	env := &Environment{SessionSecret: "0123456789abcdef0123456789abcdef", ServerMode: "magic"}

	if err := validateEnvironment(env); err == nil {
		t.Fatal("validateEnvironment(unknown mode) = nil, want error")
	}
}

func TestValidateEnvironmentRejectsWeakSessionSecret(t *testing.T) {
	cases := map[string]string{
		"empty":  "",
		"short":  "0123456789abcdef",
		"spaces": "                                ",
	}
	for name, secret := range cases {
		t.Run(name, func(t *testing.T) {
			env := &Environment{SessionSecret: secret}
			if err := validateEnvironment(env); err == nil {
				t.Fatal("validateEnvironment(weak SESSION_SECRET) = nil, want error")
			}
		})
	}
}

// TestValidateDotenvNoDuplicateKeysDetectsDuplicateAfterClosedMultiline pins
// that the multi-line tracker is reset to the top level once a quoted value
// closes: keys after a closed multi-line value must still be checked, so a real
// duplicate below one is caught rather than swallowed as value interior.
func TestValidateDotenvNoDuplicateKeysDetectsDuplicateAfterClosedMultiline(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "PASSWORD=\"first\nsecond\"\nFOO=a\nFOO=b\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateDotenvNoDuplicateKeys(path); err == nil {
		t.Fatal("validateDotenvNoDuplicateKeys(duplicate after closed multiline) = nil, want error")
	}
}

// TestValidateDotenvNoDuplicateKeysPropagatesScannerError pins that a scanner
// failure (here a line past bufio's max token size) surfaces as an error rather
// than being read as a clean, duplicate-free file.
func TestValidateDotenvNoDuplicateKeysPropagatesScannerError(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "X=" + strings.Repeat("a", 70000) // single line over bufio.MaxScanTokenSize (64KiB)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateDotenvNoDuplicateKeys(path); err == nil {
		t.Fatal("validateDotenvNoDuplicateKeys(oversized line) = nil, want scanner error")
	}
}
