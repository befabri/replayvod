package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	envparse "github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

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
	env := &Environment{ServerMode: " RELAY "}

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
	env := &Environment{}

	if err := validateEnvironment(env); err != nil {
		t.Fatalf("validateEnvironment = %v, want nil", err)
	}
	if env.ServerMode != "" {
		t.Fatalf("ServerMode = %q, want empty", env.ServerMode)
	}
	if env.ServerModeEnvConfigured {
		t.Fatal("ServerModeEnvConfigured = true, want false")
	}
}

func TestValidateEnvironmentRejectsEventSubURLsWithoutMode(t *testing.T) {
	cases := map[string]Environment{
		"webhook callback":     {WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback"},
		"relay ingest":         {RelayIngestURL: "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"},
		"relay subscribe":      {RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe"},
		"relay local callback": {RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback"},
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

func TestValidateEnvironmentRejectsUnknownServerMode(t *testing.T) {
	env := &Environment{ServerMode: "magic"}

	if err := validateEnvironment(env); err == nil {
		t.Fatal("validateEnvironment(unknown mode) = nil, want error")
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
