package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	oauthCallbackPath        = "/api/v1/auth/twitch/callback"
	defaultHTTPPort          = 8080
	legacyDefaultFrontendURL = "http://localhost:3000"
)

func validateEnvironment(env *Environment) error {
	if err := rejectLegacyURLVars(); err != nil {
		return err
	}

	env.ServerMode = strings.ToLower(strings.TrimSpace(env.ServerMode))
	env.SessionSecret = strings.TrimSpace(env.SessionSecret)
	env.CallbackURL = strings.TrimSpace(env.CallbackURL)
	env.WebhookCallbackURL = strings.TrimSpace(env.WebhookCallbackURL)
	env.FrontendURL = strings.TrimSpace(env.FrontendURL)
	env.PublicBaseURL = strings.TrimSpace(env.PublicBaseURL)
	env.RelayIngestURL = strings.TrimSpace(env.RelayIngestURL)
	env.RelaySubscribeURL = strings.TrimSpace(env.RelaySubscribeURL)
	env.RelayLocalCallbackURL = strings.TrimSpace(env.RelayLocalCallbackURL)
	env.DashboardDir = strings.TrimSpace(env.DashboardDir)

	if err := derivePublicURLs(env); err != nil {
		return err
	}

	if !ValidSessionSecret(env.SessionSecret) {
		return fmt.Errorf("SESSION_SECRET must be at least 32 characters")
	}

	env.ServerModeEnvConfigured = env.ServerMode != ""
	switch env.ServerMode {
	case "":
		// Server mode is app-managed through the owner dashboard. The URL knobs
		// only mean something paired with SERVER_MODE, so reject a
		// half-set env instead of silently ignoring it.
		if env.hasEventSubURLs() {
			return fmt.Errorf("EventSub URL env vars require SERVER_MODE to be set (off, poll, direct, or relay); unset them to manage server mode from the dashboard")
		}
	case ServerModeOff, ServerModePoll, ServerModeDirect, ServerModeRelay:
		// Valid.
	default:
		return fmt.Errorf("SERVER_MODE must be one of %q, %q, %q, or %q",
			ServerModeOff, ServerModePoll, ServerModeDirect, ServerModeRelay)
	}
	return nil
}

func rejectLegacyURLVars() error {
	for _, name := range []string{"CALLBACK_URL", "FRONTEND_URL"} {
		if raw, ok := os.LookupEnv(name); ok && strings.TrimSpace(raw) != "" {
			return fmt.Errorf("%s is no longer supported; set PUBLIC_BASE_URL to the public scheme://host URL, for example https://replayvod.example", name)
		}
	}
	return nil
}

// ValidSessionSecret reports whether s is strong enough to key session-token
// encryption. The value feeds HKDF rather than AES directly, so length is the
// operator-facing invariant: .env.example documents 32+ characters.
func ValidSessionSecret(s string) bool {
	return len(strings.TrimSpace(s)) >= 32
}

// hasEventSubURLs reports whether any EventSub callback or relay URL env var is
// set. They are meaningful only when paired with SERVER_MODE.
func (env *Environment) hasEventSubURLs() bool {
	return env.WebhookCallbackURL != "" || env.RelayIngestURL != "" || env.RelaySubscribeURL != "" || env.RelayLocalCallbackURL != ""
}

func derivePublicURLs(env *Environment) error {
	if env.PublicBaseURL != "" {
		base, err := normalizeAbsoluteBaseURL("PUBLIC_BASE_URL", env.PublicBaseURL)
		if err != nil {
			return err
		}
		env.PublicBaseURL = base

		env.CallbackURL = base + oauthCallbackPath
		env.FrontendURL = base
	}

	if env.CallbackURL == "" {
		port := env.Port
		if port <= 0 {
			port = defaultHTTPPort
		}
		env.CallbackURL = fmt.Sprintf("http://localhost:%d%s", port, oauthCallbackPath)
	}
	if err := validateOAuthCallbackURL(env.CallbackURL); err != nil {
		return err
	}

	if env.FrontendURL == "" {
		env.FrontendURL = legacyDefaultFrontendURL
	}
	frontend, err := normalizeAbsoluteBaseURL("FRONTEND_URL", env.FrontendURL)
	if err != nil {
		return err
	}
	env.FrontendURL = frontend

	return nil
}

func normalizeAbsoluteBaseURL(name, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%s must be an absolute URL like https://replayvod.example", name)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%s must use http:// or https://", name)
	}
	if strings.TrimRight(u.Path, "/") != "" {
		return "", fmt.Errorf("%s must not include a path; use a scheme://host URL like https://replayvod.example", name)
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func validateOAuthCallbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("CALLBACK_URL must be an absolute URL like https://replayvod.example%s; set PUBLIC_BASE_URL for normal deployments", oauthCallbackPath)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("CALLBACK_URL must use http:// or https://")
	}
	if u.Path != oauthCallbackPath {
		return fmt.Errorf("CALLBACK_URL path must be %s", oauthCallbackPath)
	}
	return nil
}

func validateDotenvNoDuplicateKeys(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scan := dotenvDupScan{seen: map[string]int{}}
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if lineNo == 1 {
			line = strings.TrimPrefix(line, "\ufeff") // editors may prepend a UTF-8 BOM
		}
		if err := scan.observe(path, lineNo, line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// dotenvDupScan carries duplicate-key detection state across the physical lines
// of a .env file. openQuote is the quote character of a multi-line value we are
// still inside, or 0 at the top level: godotenv folds the physical lines of a
// double- or single-quoted value into one assignment, so their interiors must
// not be mistaken for new keys (which would refuse boot on a valid .env).
type dotenvDupScan struct {
	seen      map[string]int
	openQuote byte
}

func (s *dotenvDupScan) observe(path string, lineNo int, line string) error {
	if s.openQuote != 0 {
		if containsUnescapedQuote(line, s.openQuote) {
			s.openQuote = 0
		}
		return nil
	}
	key, value, ok := dotenvKey(line)
	if !ok {
		return nil
	}
	if q, multiline := multilineValueQuote(value); multiline {
		s.openQuote = q
	}
	if firstLine, exists := s.seen[key]; exists {
		return fmt.Errorf("%s contains duplicate key %s on lines %d and %d", path, key, firstLine, lineNo)
	}
	s.seen[key] = lineNo
	return nil
}

func dotenvKey(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = stripExportPrefix(line)
	idx := strings.IndexByte(line, '=')
	if idx <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, line[idx+1:], true
}

// stripExportPrefix removes a leading "export" separated from the key by
// whitespace, matching godotenv; an "exportFOO" with no separator is a normal
// key and is returned unchanged.
func stripExportPrefix(line string) string {
	if rest, found := strings.CutPrefix(line, "export"); found && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		return strings.TrimLeft(rest, " \t")
	}
	return line
}

// multilineValueQuote reports the opening quote of a value whose closing quote
// is not on the same line, i.e. godotenv will fold the following physical lines
// into this value.
func multilineValueQuote(value string) (byte, bool) {
	value = strings.TrimLeft(value, " \t")
	if value == "" {
		return 0, false
	}
	q := value[0]
	if q != '"' && q != '\'' {
		return 0, false
	}
	if containsUnescapedQuote(value[1:], q) {
		return 0, false // closes on the same line
	}
	return q, true
}

// containsUnescapedQuote reports whether s contains an unescaped quote q.
// Double-quoted values honor backslash escapes, matching godotenv; single-
// quoted values are literal.
func containsUnescapedQuote(s string, q byte) bool {
	escaped := false
	for i := 0; i < len(s); i++ {
		switch {
		case escaped:
			escaped = false
		case s[i] == '\\' && q == '"':
			escaped = true
		case s[i] == q:
			return true
		}
	}
	return false
}
