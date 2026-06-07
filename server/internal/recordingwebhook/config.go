package recordingwebhook

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Config is the owner-facing state of the outbound recording webhook.
//
// Secret is the plaintext signing secret. It is intentionally exposed to the
// owner-only API: a receiver needs the secret to verify deliveries, and the
// owner is the one who configures that receiver. Never surface it on a
// viewer-level route.
type Config struct {
	Enabled bool
	URL     string
	Secret  string
	// Events is the normalized allowlist of event identifiers to fire. Empty
	// means "all events". The allowlist is applied authoritatively at enqueue
	// time in SQL (CreateRecordingWebhookDeliveryIfEnabled), so the dispatcher
	// never re-checks it at delivery — a row only exists if it was allowed.
	Events []string
}

// UpdateInput is the owner update payload. The secret is never accepted from
// the client: it is auto-generated server-side when the webhook is first
// enabled (compare-and-swap, never overwriting an existing one) and only ever
// rotated through the dedicated RegenerateSecret path, so a long-lived signing
// key cannot be truncated or leaked through form state.
type UpdateInput struct {
	Enabled bool
	URL     string
	Events  []string
}

// configStore is the slice of repository.Repository the config service needs.
// The secret is managed through two single-concern writes that mirror the
// EventSub hmac_secret: EnsureRecordingWebhookSecret is a compare-and-swap that
// seeds one only when the slot is empty, and SetRecordingWebhookSecret rotates
// it unconditionally. Splitting the secret off the config write removes the
// read-modify-write race the old "read current secret, then upsert it back" had.
// Kept narrow (like secrets.hmacStore) so the service is trivial to exercise
// with a fake.
type configStore interface {
	GetServerSettings(ctx context.Context) (*repository.ServerSettings, error)
	UpsertRecordingWebhookConfig(ctx context.Context, enabled bool, url, events string) (*repository.ServerSettings, error)
	EnsureRecordingWebhookSecret(ctx context.Context, secret string) error
	SetRecordingWebhookSecret(ctx context.Context, secret string) error
}

type Service struct {
	repo configStore
	log  *slog.Logger
}

func New(repo configStore, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, log: log.With("domain", "recording-webhook")}
}

// Get returns the current configuration. A missing server_settings row (fresh
// install) is a valid disabled state, not an error.
func (s *Service) Get(ctx context.Context) (Config, error) {
	row, err := s.repo.GetServerSettings(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	return configFromRow(row), nil
}

// Update validates and persists the configuration, then returns the saved
// state (including the secret). Unlike server mode, the webhook needs no
// restart: the dispatcher loads the live config on every delivery, so a save
// takes effect immediately.
//
// The URL is validated whenever it is non-empty (not only when enabling), so a
// disabled config can never silently store a malformed URL that would surprise
// the owner the moment they flip it on. A signing secret is seeded the first
// time the webhook is enabled, via a compare-and-swap that never disturbs an
// existing one.
func (s *Service) Update(ctx context.Context, input UpdateInput) (Config, error) {
	url := strings.TrimSpace(input.URL)

	events, err := normalizeEvents(input.Events)
	if err != nil {
		return Config{}, err
	}

	if input.Enabled && url == "" {
		return Config{}, invalidError{message: "webhook URL is required when the webhook is enabled"}
	}
	if url != "" {
		if err := validateURL(url); err != nil {
			return Config{}, err
		}
	}
	// An enabled webhook must name at least one event. Empty is interpreted as
	// "all events" by the enqueue-time SQL filter, which is a footgun for a
	// direct API consumer who sends [] meaning "none": the server, not the
	// dashboard, is the source of truth for this invariant, so reject it here
	// rather than rely on the UI guard. (Disabled configs may stay eventless.)
	if input.Enabled && len(events) == 0 {
		return Config{}, invalidError{message: "select at least one event when the webhook is enabled"}
	}

	// Seed a signing secret the first time the webhook is enabled. The CAS in
	// EnsureRecordingWebhookSecret makes the generated value a no-op when one
	// already exists, so this neither races a concurrent save nor rotates a
	// live key. Do this before enabling the config so a partial DB failure
	// cannot leave an enabled webhook with an empty signing secret.
	if input.Enabled {
		if _, err := s.EnsureSecret(ctx); err != nil {
			return Config{}, err
		}
	}

	if _, err := s.repo.UpsertRecordingWebhookConfig(ctx, input.Enabled, url, strings.Join(events, ",")); err != nil {
		return Config{}, err
	}
	return s.Get(ctx)
}

// EnsureSecret seeds the signing secret when the slot is empty and returns the
// saved config. It is used by both enable and SendTest so a test delivery is
// never signed with an empty key.
func (s *Service) EnsureSecret(ctx context.Context) (Config, error) {
	secret, err := generateSecret()
	if err != nil {
		return Config{}, err
	}
	if err := s.repo.EnsureRecordingWebhookSecret(ctx, secret); err != nil {
		return Config{}, err
	}
	return s.Get(ctx)
}

// RegenerateSecret rotates the signing secret unconditionally and returns the
// saved state. It is a separate path from Update so rotating a key never
// piggybacks on (or is gated by) the rest of the config form: the dashboard
// calls it on its own deliberate action, after a confirmation.
func (s *Service) RegenerateSecret(ctx context.Context) (Config, error) {
	secret, err := generateSecret()
	if err != nil {
		return Config{}, err
	}
	if err := s.repo.SetRecordingWebhookSecret(ctx, secret); err != nil {
		return Config{}, err
	}
	return s.Get(ctx)
}

func configFromRow(row *repository.ServerSettings) Config {
	return Config{
		Enabled: row.RecordingWebhookEnabled,
		URL:     row.RecordingWebhookURL,
		Secret:  row.RecordingWebhookSecret,
		Events:  parseEvents(row.RecordingWebhookEvents),
	}
}
