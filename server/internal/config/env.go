package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func validateEnvironment(env *Environment) error {
	env.ServerMode = strings.ToLower(strings.TrimSpace(env.ServerMode))
	env.WebhookCallbackURL = strings.TrimSpace(env.WebhookCallbackURL)
	env.RelayIngestURL = strings.TrimSpace(env.RelayIngestURL)
	env.RelaySubscribeURL = strings.TrimSpace(env.RelaySubscribeURL)
	env.RelayLocalCallbackURL = strings.TrimSpace(env.RelayLocalCallbackURL)

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

// hasEventSubURLs reports whether any EventSub callback or relay URL env var is
// set. They are meaningful only when paired with SERVER_MODE.
func (env *Environment) hasEventSubURLs() bool {
	return env.WebhookCallbackURL != "" || env.RelayIngestURL != "" || env.RelaySubscribeURL != "" || env.RelayLocalCallbackURL != ""
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
