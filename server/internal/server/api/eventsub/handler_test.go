package eventsub

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/eventsubconfig"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/trpcgo"
)

// requireTRPCCode asserts err is a *trpcgo.Error carrying the wanted code, so
// the handler tests pin the wire status (400 vs 500) clients actually receive,
// not just "an error happened".
func requireTRPCCode(t *testing.T, err error, want trpcgo.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want tRPC code %v", want)
	}
	var trpcErr *trpcgo.Error
	if !errors.As(err, &trpcErr) {
		t.Fatalf("error = %T (%v), want *trpcgo.Error", err, err)
	}
	if trpcErr.Code != want {
		t.Fatalf("tRPC code = %v, want %v", trpcErr.Code, want)
	}
}

func newConfigHandler(t *testing.T, eventSub config.ServerModeConfig, development bool) (*Handler, repository.Repository, *config.Config) {
	t.Helper()

	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	cfg := &config.Config{
		App: config.AppConfig{
			Development: development,
		},
		Env:        config.Environment{HMACSecret: "0123456789abcdef"},
		ServerMode: eventSub,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	configSvc := eventsubconfig.New(repo, cfg, log)
	return NewHandler(nil, configSvc, log), repo, cfg
}

func TestConfig_AppManagedWithoutSettingsRequiresSetup(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	got, err := h.Config(ctx)
	if err != nil {
		t.Fatalf("Config() error = %v, want nil", err)
	}
	if got.Source != config.ServerModeConfigSourceUnset {
		t.Fatalf("Source = %q, want %q", got.Source, config.ServerModeConfigSourceUnset)
	}
	if !got.SetupRequired {
		t.Fatal("SetupRequired = false, want true")
	}
	if got.RestartRequired {
		t.Fatal("RestartRequired = true, want false")
	}
	if got.CreatesTwitchSubscriptions {
		t.Fatal("CreatesTwitchSubscriptions = true, want false")
	}
}

func TestUpdateConfig_OffClearsURLsAndRequiresRestartFromSetup(t *testing.T) {
	ctx := context.Background()
	h, repo, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	got, err := h.UpdateConfig(ctx, UpdateConfigInput{
		Mode:                  config.ServerModeOff,
		WebhookCallbackURL:    "https://replayvod.example/api/v1/webhook/callback",
		RelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("UpdateConfig(off) error = %v, want nil", err)
	}
	if got.Mode != config.ServerModeOff {
		t.Fatalf("Mode = %q, want %q", got.Mode, config.ServerModeOff)
	}
	if !got.RestartRequired {
		t.Fatal("RestartRequired = false, want true")
	}
	if got.WebhookCallbackURL != "" || got.RelayIngestURL != "" || got.RelaySubscribeURL != "" || got.RelayLocalCallbackURL != "" {
		t.Fatalf("response URLs were not cleared: %#v", got)
	}

	row, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings() error = %v, want nil", err)
	}
	if row.EventSubWebhookCallbackURL != "" || row.EventSubRelayIngestURL != "" || row.EventSubRelaySubscribeURL != "" || row.EventSubRelayLocalCallbackURL != "" {
		t.Fatalf("stored URLs were not cleared: %#v", row)
	}
}

func TestUpdateConfig_PersistsRelayAndReportsRestartRequired(t *testing.T) {
	ctx := context.Background()
	h, repo, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	got, err := h.UpdateConfig(ctx, UpdateConfigInput{
		Mode:                  config.ServerModeRelay,
		WebhookCallbackURL:    "https://replayvod.example/api/v1/webhook/callback",
		RelayIngestURL:        " https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA ",
		RelaySubscribeURL:     " wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe ",
		RelayLocalCallbackURL: " http://127.0.0.1:8080/api/v1/webhook/callback ",
	})
	if err != nil {
		t.Fatalf("UpdateConfig(relay) error = %v, want nil", err)
	}
	if !got.RestartRequired {
		t.Fatal("RestartRequired = false, want true")
	}
	if !got.CreatesTwitchSubscriptions {
		t.Fatal("CreatesTwitchSubscriptions = false, want true")
	}
	if !got.UsesRelayAgent {
		t.Fatal("UsesRelayAgent = false, want true")
	}
	if got.WebhookCallbackURL != "" {
		t.Fatalf("WebhookCallbackURL = %q, want cleared for relay", got.WebhookCallbackURL)
	}
	if got.RelayIngestURL != "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA" {
		t.Fatalf("RelayIngestURL = %q, want trimmed relay ingest URL", got.RelayIngestURL)
	}

	row, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings() error = %v, want nil", err)
	}
	if row.EventSubWebhookCallbackURL != "" {
		t.Fatalf("stored WebhookCallbackURL = %q, want cleared for relay", row.EventSubWebhookCallbackURL)
	}
	if row.EventSubRelaySubscribeURL != "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe" {
		t.Fatalf("stored RelaySubscribeURL = %q, want trimmed subscribe URL", row.EventSubRelaySubscribeURL)
	}

	reloaded, err := h.Config(ctx)
	if err != nil {
		t.Fatalf("Config() after update error = %v, want nil", err)
	}
	if !reloaded.RestartRequired {
		t.Fatal("Config() RestartRequired = false, want true")
	}
	if reloaded.Active.Source != config.ServerModeConfigSourceUnset {
		t.Fatalf("Active.Source = %q, want %q", reloaded.Active.Source, config.ServerModeConfigSourceUnset)
	}
}

// TestConfig_SavedCapabilitiesDoNotLeakIntoActiveRuntime pins the saved-vs-
// active split. After saving relay onto a process that booted unconfigured, the
// saved config advertises subscription creation, but the active runtime — what
// SubscribeStreamOnline actually gates on — does not, until a restart. A client
// reads Active to know what works now and the top-level fields to know what was
// saved.
func TestConfig_SavedCapabilitiesDoNotLeakIntoActiveRuntime(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	got, err := h.UpdateConfig(ctx, UpdateConfigInput{
		Mode:              config.ServerModeRelay,
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	})
	if err != nil {
		t.Fatalf("UpdateConfig(relay) error = %v, want nil", err)
	}
	if !got.RestartRequired {
		t.Fatal("RestartRequired = false, want true")
	}
	if !got.CreatesTwitchSubscriptions || !got.UsesRelayAgent {
		t.Fatalf("saved capabilities = %+v, want creates+relay true", got)
	}
	if got.Active.CreatesTwitchSubscriptions || got.Active.UsesRelayAgent {
		t.Fatalf("active capabilities = %+v, want both false until restart", got.Active)
	}
	if got.Active.Mode != "" || got.Active.Source != config.ServerModeConfigSourceUnset {
		t.Fatalf("active runtime = %+v, want unset until restart", got.Active)
	}
}

func TestSubscribeStreamOnlineRejectsWhenSavedCanCreateButActiveCannot(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	got, err := h.UpdateConfig(ctx, UpdateConfigInput{
		Mode:              config.ServerModeRelay,
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	})
	if err != nil {
		t.Fatalf("UpdateConfig(relay) error = %v, want nil", err)
	}
	if !got.CreatesTwitchSubscriptions {
		t.Fatal("saved CreatesTwitchSubscriptions = false, want true")
	}
	if got.Active.CreatesTwitchSubscriptions {
		t.Fatal("active CreatesTwitchSubscriptions = true, want false until restart")
	}

	_, err = h.SubscribeStreamOnline(ctx, SubscribeInput{BroadcasterID: "12345"})
	if err == nil {
		t.Fatal("SubscribeStreamOnline() error = nil, want active-runtime rejection")
	}
	if !strings.Contains(err.Error(), "not configured for Twitch subscriptions") {
		t.Fatalf("SubscribeStreamOnline() error = %v, want active-runtime rejection", err)
	}
}

// TestUpdateConfig_DirectResponseEmitsOnlyItsOwnURL pins that the response emits
// only the URL fields the saved delivery uses. stateToResponse no longer filters
// per delivery — it relies on the config being URL-cleared at the source — so a
// direct save must surface the webhook URL and drop stray relay URLs.
func TestUpdateConfig_DirectResponseEmitsOnlyItsOwnURL(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	got, err := h.UpdateConfig(ctx, UpdateConfigInput{
		Mode:               config.ServerModeDirect,
		WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback",
		// Stray relay URLs from a prior mode must be cleared, not leaked.
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	})
	if err != nil {
		t.Fatalf("UpdateConfig(direct) error = %v, want nil", err)
	}
	if got.WebhookCallbackURL != "https://replayvod.example/api/v1/webhook/callback" {
		t.Fatalf("WebhookCallbackURL = %q, want the saved direct callback", got.WebhookCallbackURL)
	}
	if got.RelayIngestURL != "" || got.RelaySubscribeURL != "" || got.RelayLocalCallbackURL != "" {
		t.Fatalf("response leaked relay URLs on a direct config: %#v", got)
	}
}

func TestUpdateConfig_RejectsInvalidRelayWithoutPersisting(t *testing.T) {
	ctx := context.Background()
	h, repo, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	_, err := h.UpdateConfig(ctx, UpdateConfigInput{
		Mode:              config.ServerModeRelay,
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/BBBBBBBBBBBBBBBB/subscribe",
	})
	requireTRPCCode(t, err, trpcgo.CodeBadRequest)
	_, err = repo.GetServerSettings(ctx)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings() after rejected update = %v, want ErrNotFound", err)
	}
}

// TestUpdateConfig_RejectsEmptyMode pins that an explicit owner update must name
// a mode. ValidateServerMode treats an empty mode as the valid unset/onboarding
// state, so without an owner-write-boundary guard an empty mode would silently
// persist a meaningless setup-required row. The tRPC oneof tag also blocks this,
// but only for dispatched requests; this asserts the domain service defends
// callers that bypass dispatch.
func TestUpdateConfig_RejectsEmptyMode(t *testing.T) {
	ctx := context.Background()
	h, repo, _ := newConfigHandler(t, config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}, false)

	_, err := h.UpdateConfig(ctx, UpdateConfigInput{Mode: ""})
	requireTRPCCode(t, err, trpcgo.CodeBadRequest)
	if _, err := repo.GetServerSettings(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings() after rejected empty-mode update = %v, want ErrNotFound", err)
	}
}

func TestUpdateConfig_EnvManagedRejectsAppUpdates(t *testing.T) {
	ctx := context.Background()
	h, repo, _ := newConfigHandler(t, config.ServerModeConfig{
		Source:                config.ServerModeConfigSourceEnv,
		Mode:                  config.ServerModeRelay,
		RelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	}, false)

	got, err := h.Config(ctx)
	if err != nil {
		t.Fatalf("Config() error = %v, want nil", err)
	}
	if !got.EnvManaged {
		t.Fatal("EnvManaged = false, want true")
	}

	_, err = h.UpdateConfig(ctx, UpdateConfigInput{Mode: config.ServerModeOff})
	requireTRPCCode(t, err, trpcgo.CodeBadRequest)
	_, err = repo.GetServerSettings(ctx)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings() after rejected env-managed update = %v, want ErrNotFound", err)
	}
}

func TestRestartRequired_SetupRequiredToOffRequiresRestart(t *testing.T) {
	active := config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}
	saved := config.ServerModeConfig{
		Source: config.ServerModeConfigSourceApp,
		Mode:   config.ServerModeOff,
	}

	if !eventsubconfig.RestartRequired(active, saved) {
		t.Fatal("restartRequired(setup required -> off) = false, want true")
	}
}

func TestRestartRequired_SetupRequiredToRelayRequiresRestart(t *testing.T) {
	active := config.ServerModeConfig{
		Source: config.ServerModeConfigSourceUnset,
	}
	saved := config.ServerModeConfig{
		Source:                config.ServerModeConfigSourceApp,
		Mode:                  config.ServerModeRelay,
		RelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	}

	if !eventsubconfig.RestartRequired(active, saved) {
		t.Fatal("restartRequired(setup required -> relay) = false, want true")
	}
}

func TestRestartRequired_EquivalentRuntimeDoesNotRequireRestart(t *testing.T) {
	active := config.ServerModeConfig{
		Source: config.ServerModeConfigSourceEnv,
		Mode:   config.ServerModeOff,
	}
	saved := config.ServerModeConfig{
		Source: config.ServerModeConfigSourceApp,
		Mode:   config.ServerModeOff,
	}

	if eventsubconfig.RestartRequired(active, saved) {
		t.Fatal("restartRequired(equivalent runtime) = true, want false")
	}
}
