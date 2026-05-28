package eventsubconfig

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type fakeRevoker struct {
	calls  int
	reason string
	count  int
	err    error
}

func (r *fakeRevoker) RevokeAllActive(_ context.Context, reason string) (int, error) {
	r.calls++
	r.reason = reason
	return r.count, r.err
}

// failingUpsertRepo embeds a real repository but forces UpsertServerSettings to
// fail, so a test can exercise the "save failed" branch of Update without a
// flaky real-DB failure. Every other method delegates to the embedded repo.
type failingUpsertRepo struct {
	repository.Repository
	err error
}

func (r *failingUpsertRepo) UpsertServerSettings(context.Context, *repository.ServerSettings) (*repository.ServerSettings, error) {
	return nil, r.err
}

func newTestService(t *testing.T, active config.ServerModeConfig) (*Service, repository.Repository, *config.Config) {
	t.Helper()

	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	cfg := &config.Config{
		App:        config.AppConfig{},
		Env:        config.Environment{HMACSecret: "0123456789abcdef"},
		ServerMode: active,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(repo, cfg, log), repo, cfg
}

func TestResolve_AppManagedRelayFromServerSettings(t *testing.T) {
	ctx := context.Background()
	_, repo, cfg := newTestService(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	})
	_, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode:                    config.ServerModeRelay,
		EventSubRelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		EventSubRelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		EventSubRelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}

	got, err := Resolve(ctx, repo, cfg)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got.Source != config.ServerModeConfigSourceApp {
		t.Fatalf("Source = %q, want %q", got.Source, config.ServerModeConfigSourceApp)
	}
	if got.Mode != config.ServerModeRelay {
		t.Fatalf("Mode = %q, want relay", got.Mode)
	}
	if !got.CreatesTwitchSubscriptions() {
		t.Fatal("CreatesTwitchSubscriptions = false, want true")
	}
}

// TestResolve_EnvManagedWinsOverSaved pins that env-managed config is
// authoritative at boot: a persisted owner row is ignored when env owns config.
func TestResolve_EnvManagedWinsOverSaved(t *testing.T) {
	ctx := context.Background()
	_, repo, cfg := newTestService(t, config.ServerModeConfig{
		Source:             config.ServerModeConfigSourceEnv,
		Mode:               config.ServerModeDirect,
		WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback",
	})
	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode:                config.ServerModeRelay,
		EventSubRelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		EventSubRelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}

	got, err := Resolve(ctx, repo, cfg)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got.Source != config.ServerModeConfigSourceEnv || got.Mode != config.ServerModeDirect {
		t.Fatalf("Resolve() = %+v, want env-managed direct (saved relay ignored)", got)
	}
}

// TestResolve_EnvManagedInvalidReturnsFatalInvalidError pins the boot boundary:
// env-managed config is authoritative, but if it is invalid Resolve must return
// ErrInvalid instead of degrading to setup-required or falling back to a saved
// app row. main.resolveOrDegrade treats this branch as fatal.
func TestResolve_EnvManagedInvalidReturnsFatalInvalidError(t *testing.T) {
	ctx := context.Background()
	_, repo, cfg := newTestService(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceEnv,
		Mode:   config.ServerModeDirect,
		// Missing WebhookCallbackURL makes the env runtime invalid.
	})
	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode: config.ServerModeOff,
	}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}

	got, err := Resolve(ctx, repo, cfg)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Resolve() error = %v, want ErrInvalid", err)
	}
	if got.Source != config.ServerModeConfigSourceEnv || got.Mode != config.ServerModeDirect {
		t.Fatalf("Resolve() runtime = %+v, want the invalid env config returned for diagnostics", got)
	}
}

func TestResolve_AppManagedRelayRequiresHMACSecret(t *testing.T) {
	ctx := context.Background()
	_, repo, cfg := newTestService(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	})
	cfg.Env.HMACSecret = ""
	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode:                config.ServerModeRelay,
		EventSubRelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		EventSubRelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}

	_, err := Resolve(ctx, repo, cfg)
	if err == nil {
		t.Fatal("Resolve(relay without HMAC secret) error = nil, want error")
	}
}

func TestState_InvalidSavedConfigReportsSetupRequired(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	})
	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode: config.ServerModeRelay,
	}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}

	state, err := svc.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v, want nil", err)
	}
	if !state.Saved.SetupRequired() {
		t.Fatalf("State().Saved = %+v, want setup-required fallback", state.Saved)
	}
	if state.RestartRequired {
		t.Fatal("RestartRequired = true, want false for setup-required fallback over unset active")
	}
}

func TestUpdate_RelaySanitizesAndRequiresRestart(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	})

	state, err := svc.Update(ctx, UpdateInput{
		Mode:                  config.ServerModeRelay,
		WebhookCallbackURL:    "https://replayvod.example/api/v1/webhook/callback",
		RelayIngestURL:        " https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA ",
		RelaySubscribeURL:     " wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe ",
		RelayLocalCallbackURL: " http://127.0.0.1:8080/api/v1/webhook/callback ",
	})
	if err != nil {
		t.Fatalf("Update(relay) error = %v, want nil", err)
	}
	if !state.RestartRequired {
		t.Fatal("RestartRequired = false, want true")
	}
	if state.Saved.WebhookCallbackURL != "" {
		t.Fatalf("Saved.WebhookCallbackURL = %q, want cleared", state.Saved.WebhookCallbackURL)
	}
	if state.Saved.RelayIngestURL != "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA" {
		t.Fatalf("RelayIngestURL = %q, want trimmed", state.Saved.RelayIngestURL)
	}

	row, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if row.EventSubWebhookCallbackURL != "" {
		t.Fatalf("stored WebhookCallbackURL = %q, want cleared", row.EventSubWebhookCallbackURL)
	}
	if row.EventSubRelaySubscribeURL != "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe" {
		t.Fatalf("stored RelaySubscribeURL = %q, want trimmed", row.EventSubRelaySubscribeURL)
	}
}

func TestUpdate_RejectsWebhookModeWithoutHMACSecret(t *testing.T) {
	ctx := context.Background()
	svc, repo, cfg := newTestService(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	})
	cfg.Env.HMACSecret = ""

	_, err := svc.Update(ctx, UpdateInput{
		Mode:              config.ServerModeRelay,
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Update(relay without HMAC secret) error = %v, want ErrInvalid", err)
	}
	_, err = repo.GetServerSettings(ctx)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings after rejected update = %v, want ErrNotFound", err)
	}
}

// TestUpdate_RejectsEmptyMode pins the owner-write-boundary guard: an explicit
// update with no mode is rejected as ErrInvalid and persists nothing.
// ValidateServerMode deliberately accepts an empty mode as the unset/onboarding
// state (env config and boot resolution rely on that), so this rule has to live
// in Update, not the shared validator.
func TestUpdate_RejectsEmptyMode(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	})

	_, err := svc.Update(ctx, UpdateInput{Mode: "  "})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Update(empty mode) error = %v, want ErrInvalid", err)
	}
	if _, err := repo.GetServerSettings(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings after rejected empty-mode update = %v, want ErrNotFound", err)
	}
}

func TestUpdate_OffPersistsRestartAppliedConfigAndClearsURLs(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService(t, config.ServerModeConfig{
		Source:                config.ServerModeConfigSourceApp,
		Mode:                  config.ServerModeRelay,
		RelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	})

	state, err := svc.Update(ctx, UpdateInput{
		Mode:                  config.ServerModeOff,
		WebhookCallbackURL:    "https://replayvod.example/api/v1/webhook/callback",
		RelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("Update(off) error = %v, want nil", err)
	}
	if state.Saved.Mode != config.ServerModeOff {
		t.Fatalf("Mode = %q, want off", state.Saved.Mode)
	}
	if state.Active.Mode != config.ServerModeRelay {
		t.Fatalf("Active.Mode = %q, want still-running relay runtime", state.Active.Mode)
	}
	if !state.RestartRequired {
		t.Fatal("RestartRequired = false, want true for active relay -> saved off")
	}
	if state.Saved.WebhookCallbackURL != "" || state.Saved.RelayIngestURL != "" ||
		state.Saved.RelaySubscribeURL != "" || state.Saved.RelayLocalCallbackURL != "" {
		t.Fatalf("off config retained URLs: %#v", state.Saved)
	}

	row, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if row.ServerMode != config.ServerModeOff {
		t.Fatalf("stored mode = %q, want off", row.ServerMode)
	}
}

func TestUpdate_PersistFailureDoesNotSaveConfig(t *testing.T) {
	ctx := context.Background()
	repo := &failingUpsertRepo{
		Repository: sqliteadapter.New(testdb.NewSQLiteDB(t)),
		err:        errors.New("server_settings write failed"),
	}
	cfg := &config.Config{
		Env: config.Environment{HMACSecret: "0123456789abcdef"},
		ServerMode: config.ServerModeConfig{
			Source:            config.ServerModeConfigSourceApp,
			Mode:              config.ServerModeRelay,
			RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
			RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(repo, cfg, log)

	if _, err := svc.Update(ctx, UpdateInput{Mode: config.ServerModeOff}); err == nil {
		t.Fatal("Update(off) with failing persist = nil error, want save failure")
	}
}

func TestCleanupNonSubscriptionRuntimeRevokesWhenActiveCannotCreateTwitchSubscriptions(t *testing.T) {
	ctx := context.Background()
	revoker := &fakeRevoker{count: 3}

	err := CleanupNonSubscriptionRuntime(ctx, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceApp,
		Mode:   config.ServerModeOff,
	}, revoker, nil)
	if err != nil {
		t.Fatalf("CleanupNonSubscriptionRuntime(off) error = %v, want nil", err)
	}
	if revoker.calls != 1 {
		t.Fatalf("RevokeAllActive calls = %d, want 1", revoker.calls)
	}
	if revoker.reason != subscriptionCleanupReason {
		t.Fatalf("revoke reason = %q, want %q", revoker.reason, subscriptionCleanupReason)
	}

	revoker.calls = 0
	err = CleanupNonSubscriptionRuntime(ctx, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, revoker, nil)
	if err != nil {
		t.Fatalf("CleanupNonSubscriptionRuntime(unset) error = %v, want nil", err)
	}
	if revoker.calls != 1 {
		t.Fatalf("RevokeAllActive calls = %d, want 1 for unset runtime", revoker.calls)
	}

	revoker.calls = 0
	err = CleanupNonSubscriptionRuntime(ctx, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceApp,
		Mode:   config.ServerModePoll,
	}, revoker, nil)
	if err != nil {
		t.Fatalf("CleanupNonSubscriptionRuntime(poll) error = %v, want nil", err)
	}
	if revoker.calls != 1 {
		t.Fatalf("RevokeAllActive calls = %d, want 1 for poll runtime", revoker.calls)
	}

	revoker.calls = 0
	err = CleanupNonSubscriptionRuntime(ctx, config.ServerModeConfig{
		Source:            config.ServerModeConfigSourceApp,
		Mode:              config.ServerModeRelay,
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	}, revoker, nil)
	if err != nil {
		t.Fatalf("CleanupNonSubscriptionRuntime(relay) error = %v, want nil", err)
	}
	if revoker.calls != 0 {
		t.Fatalf("RevokeAllActive calls = %d, want 0 for enabled runtime", revoker.calls)
	}
}

func TestCleanupNonSubscriptionRuntimeReportsFailure(t *testing.T) {
	ctx := context.Background()
	cleanupErr := errors.New("twitch unavailable")
	revoker := &fakeRevoker{err: cleanupErr}

	err := CleanupNonSubscriptionRuntime(ctx, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceApp,
		Mode:   config.ServerModeOff,
	}, revoker, nil)
	if !errors.Is(err, ErrSubscriptionCleanup) {
		t.Fatalf("CleanupNonSubscriptionRuntime(off) error = %v, want ErrSubscriptionCleanup", err)
	}
	if revoker.calls != 1 {
		t.Fatalf("RevokeAllActive calls = %d, want 1", revoker.calls)
	}
}

func TestRestartRequired(t *testing.T) {
	active := config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}
	off := config.ServerModeConfig{
		Source: config.ServerModeConfigSourceApp,
		Mode:   config.ServerModeOff,
	}
	relay := config.ServerModeConfig{
		Source:                config.ServerModeConfigSourceApp,
		Mode:                  config.ServerModeRelay,
		RelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	}

	if !RestartRequired(active, off) {
		t.Fatal("RestartRequired(setup -> off) = false, want true")
	}
	if !RestartRequired(active, relay) {
		t.Fatal("RestartRequired(setup -> relay) = false, want true")
	}
	if RestartRequired(config.ServerModeConfig{Mode: config.ServerModeOff}, off) {
		t.Fatal("RestartRequired(equivalent off) = true, want false")
	}
}
