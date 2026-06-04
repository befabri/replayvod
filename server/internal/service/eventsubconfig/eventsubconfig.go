// Package eventsubconfig owns server mode configuration.
//
// It is deliberately separate from service/eventsub: that package manages
// Twitch subscriptions and snapshots, while this package resolves how Twitch
// live/title signals should reach this process and persists owner-managed
// settings.
package eventsubconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
)

const subscriptionCleanupReason = "server mode does not create Twitch subscriptions"

var (
	ErrEnvManaged          = errors.New("server mode config: env managed")
	ErrInvalid             = errors.New("server mode config: invalid")
	ErrSubscriptionCleanup = errors.New("server mode config: subscription cleanup failed")
)

// Revoker is the subscription-management slice needed when the active runtime is
// not allowed to create Twitch subscriptions. service/eventsub.Service implements
// it in production; tests can use a fake without stubbing Twitch.
type Revoker interface {
	RevokeAllActive(ctx context.Context, reason string) (int, error)
}

// Service resolves runtime config and persists owner-managed config.
type Service struct {
	repo repository.Repository
	cfg  *config.Config
	log  *slog.Logger
}

func New(repo repository.Repository, cfg *config.Config, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		repo: repo,
		cfg:  cfg,
		log:  log.With("domain", "eventsub-config"),
	}
}

// UpdateInput is the domain-shaped owner update payload.
type UpdateInput struct {
	Mode                  string
	WebhookCallbackURL    string
	RelayIngestURL        string
	RelaySubscribeURL     string
	RelayLocalCallbackURL string
}

// State is the owner-facing config state before transport DTO mapping.
type State struct {
	Saved           config.ServerModeConfig
	Active          config.ServerModeConfig
	RestartRequired bool
	// DirectCallbackURL is the callback URL direct mode would use, derived from
	// the server's public base URL regardless of the current mode. The dashboard
	// shows it read-only when the owner picks direct (they enter nothing); empty
	// means no public base is configured.
	DirectCallbackURL string
}

// Active returns the server mode config currently running in this process.
func (s *Service) Active() config.ServerModeConfig {
	active := s.cfg.ServerMode
	active.Normalize()
	return active
}

// Resolve returns the server mode config this process should run with. Env-managed
// config wins; otherwise the persisted owner config is used. A missing owner
// config is valid and means onboarding is required.
//
// It is a package function rather than a Service method because boot needs this
// read-only resolution before the owner-facing API service is wired.
func Resolve(ctx context.Context, repo repository.Repository, cfg *config.Config) (config.ServerModeConfig, error) {
	if cfg.ServerMode.EnvManaged() {
		runtime := cfg.ServerMode
		runtime.Normalize()
		runtime.ResolveDerivedURLs(cfg.PublicAPIBaseURL())
		if err := config.ValidateServerModeRuntimeConfig(runtime, cfg.Env.HMACSecret); err != nil {
			return runtime, invalidError{message: err.Error()}
		}
		return runtime, nil
	}

	runtime, err := loadSavedConfig(ctx, repo)
	if err != nil {
		return config.ServerModeConfig{}, err
	}
	// Fill the URLs the owner no longer supplies (relay subscribe, direct
	// callback) before validating and before this becomes the process runtime.
	runtime.ResolveDerivedURLs(cfg.PublicAPIBaseURL())
	if err := config.ValidateServerModeRuntimeConfig(runtime, cfg.Env.HMACSecret); err != nil {
		return runtime, invalidError{message: err.Error()}
	}
	return runtime, nil
}

// State returns saved config plus active runtime config. Env-managed config is
// both the saved and active state because environment variables are authoritative.
func (s *Service) State(ctx context.Context) (State, error) {
	active := s.Active()
	if active.EnvManaged() {
		return State{
			Saved:             active,
			Active:            active,
			DirectCallbackURL: config.PublicWebhookCallbackURL(s.cfg.PublicAPIBaseURL()),
		}, nil
	}

	saved, err := loadSavedConfig(ctx, s.repo)
	if err != nil {
		return State{}, err
	}
	saved.ResolveDerivedURLs(s.cfg.PublicAPIBaseURL())
	if err := config.ValidateServerModeRuntimeConfig(saved, s.cfg.Env.HMACSecret); err != nil {
		s.log.Warn("saved server mode config invalid; reporting setup required", "error", err, "mode", saved.Mode)
		saved = config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset}
	}
	return s.stateFromSaved(saved), nil
}

// Update validates and persists owner-managed server mode setup. The running
// process is not hot-reloaded: relay clients, webhook processors, scheduler
// runners, downloader subscribers, and Twitch subscriptions are wired from the
// active config at boot. Callers must use RestartRequired to tell the owner when
// the saved config differs from what this process is currently running.
func (s *Service) Update(ctx context.Context, input UpdateInput) (State, error) {
	if s.cfg.ServerMode.EnvManaged() {
		return State{}, ErrEnvManaged
	}

	// ServerModeConfigFromApp normalizes and clears the URL fields the chosen
	// mode does not use, so the stored row is already canonical.
	desired := config.ServerModeConfigFromApp(
		input.Mode,
		input.WebhookCallbackURL,
		input.RelayIngestURL,
		input.RelaySubscribeURL,
		input.RelayLocalCallbackURL,
	)
	// An explicit owner update must name a mode. ValidateServerMode treats an
	// empty mode as the valid unset/onboarding state (env config and boot
	// resolution depend on that), so the "you must choose a mode" rule lives
	// here at the owner-write boundary rather than in the shared validator. The
	// tRPC `oneof` tag enforces this too, but only for dispatched requests;
	// keeping it here defends every caller of the domain service.
	if desired.Mode == "" {
		return State{}, invalidError{message: "server mode is required"}
	}
	// Validate against the resolved config (relay subscribe + direct callback
	// filled in) but persist only what the owner supplied: the direct callback
	// is re-derived from the public base on every read, so it is never stored
	// stale if the public base later changes.
	resolved := desired
	resolved.ResolveDerivedURLs(s.cfg.PublicAPIBaseURL())
	if err := config.ValidateServerModeRuntimeConfig(resolved, s.cfg.Env.HMACSecret); err != nil {
		return State{}, invalidError{message: err.Error()}
	}

	row, err := s.repo.UpsertServerSettings(ctx, serverSettingsFromConfig(desired))
	if err != nil {
		return State{}, fmt.Errorf("save server mode config: %w", err)
	}
	return s.stateFromSaved(serverSettingsToConfig(row)), nil
}

// CleanupNonSubscriptionRuntime applies a runtime that cannot create Twitch
// subscriptions after boot wiring has an EventSub revoker. It is deliberately
// separate from Update: saved settings are restart-applied, while this function
// runs only for the config this process actually booted with.
func CleanupNonSubscriptionRuntime(ctx context.Context, active config.ServerModeConfig, revoker Revoker, log *slog.Logger) error {
	active.Normalize()
	if active.CreatesTwitchSubscriptions() || revoker == nil {
		return nil
	}
	revoked, err := revoker.RevokeAllActive(ctx, subscriptionCleanupReason)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSubscriptionCleanup, err)
	}
	if revoked > 0 {
		if log == nil {
			log = slog.Default()
		}
		log.With("domain", "eventsub-config").Info("revoked active EventSub subscriptions after non-subscription runtime booted", "count", revoked)
	}
	return nil
}

func loadSavedConfig(ctx context.Context, repo repository.Repository) (config.ServerModeConfig, error) {
	row, err := repo.GetServerSettings(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset}, nil
	}
	if err != nil {
		return config.ServerModeConfig{}, fmt.Errorf("load server settings: %w", err)
	}
	return serverSettingsToConfig(row), nil
}

func (s *Service) stateFromSaved(saved config.ServerModeConfig) State {
	// Resolve derived URLs so the response (and the restart-required diff) sees
	// the same effective config the runtime will, including the direct callback
	// derived from the public base.
	saved.ResolveDerivedURLs(s.cfg.PublicAPIBaseURL())
	active := s.Active()
	return State{
		Saved:             saved,
		Active:            active,
		RestartRequired:   RestartRequired(active, saved),
		DirectCallbackURL: config.PublicWebhookCallbackURL(s.cfg.PublicAPIBaseURL()),
	}
}

func serverSettingsToConfig(s *repository.ServerSettings) config.ServerModeConfig {
	if s == nil {
		return config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset}
	}
	return config.ServerModeConfigFromApp(
		s.ServerMode,
		s.EventSubWebhookCallbackURL,
		s.EventSubRelayIngestURL,
		s.EventSubRelaySubscribeURL,
		s.EventSubRelayLocalCallbackURL,
	)
}

func serverSettingsFromConfig(cfg config.ServerModeConfig) *repository.ServerSettings {
	return &repository.ServerSettings{
		ServerMode:                    cfg.Mode,
		EventSubWebhookCallbackURL:    cfg.WebhookCallbackURL,
		EventSubRelayIngestURL:        cfg.RelayIngestURL,
		EventSubRelaySubscribeURL:     cfg.RelaySubscribeURL,
		EventSubRelayLocalCallbackURL: cfg.RelayLocalCallbackURL,
	}
}

// RestartRequired reports whether the saved config differs from what this
// process is currently running, i.e. the owner must restart for the saved
// config to take effect. This is intentionally pure: cleanup of stale Twitch
// subscriptions is restart-applied too, so setup-required -> off/poll still
// reports a restart until the process has booted with the saved runtime.
func RestartRequired(active, saved config.ServerModeConfig) bool {
	active.Normalize()
	saved.Normalize()
	if active.RuntimeEqual(saved) {
		return false
	}
	return true
}

type invalidError struct {
	message string
}

func (e invalidError) Error() string {
	return e.message
}

func (e invalidError) Is(target error) bool {
	return target == ErrInvalid
}
