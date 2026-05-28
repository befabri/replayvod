package eventsubconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	eventsubsvc "github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type restartRoundTripFunc func(*http.Request) (*http.Response, error)

func (f restartRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestRestartAppliedEventSubSettings exercises the real restart boundary:
// owner-saved server_settings are inert until a fresh config is resolved, and a
// later restart into a non-subscription runtime revokes active Twitch
// subscriptions left by the previous relay runtime.
func TestRestartAppliedEventSubSettings(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	const (
		broadcasterID = "b-restart-eventsub"
		relayIngest   = "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"
		relaySub      = "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe"
	)
	seedRestartChannel(t, ctx, repo, broadcasterID)

	boot0 := restartConfig(config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset})
	settingsSvc := New(repo, boot0, log)
	savedRelay, err := settingsSvc.Update(ctx, UpdateInput{
		Mode:                  config.ServerModeRelay,
		RelayIngestURL:        relayIngest,
		RelaySubscribeURL:     relaySub,
		RelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("Update(relay) error = %v, want nil", err)
	}
	if !savedRelay.RestartRequired {
		t.Fatal("saving relay on an unconfigured process must require restart")
	}
	if boot0.ServerMode.Mode != "" {
		t.Fatalf("active boot0 mode = %q, want still unconfigured until restart", boot0.ServerMode.Mode)
	}

	boot1 := restartConfig(config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset})
	resolved, err := Resolve(ctx, repo, boot1)
	if err != nil {
		t.Fatalf("Resolve() after relay save error = %v, want nil", err)
	}
	boot1.ServerMode = resolved
	if boot1.ServerMode.Mode != config.ServerModeRelay {
		t.Fatalf("restart resolved mode = %q, want relay", boot1.ServerMode.Mode)
	}
	if boot1.ServerModeCallbackURL() != relayIngest {
		t.Fatalf("restart callback URL = %q, want relay ingest %q", boot1.ServerModeCallbackURL(), relayIngest)
	}

	created := map[string]string{}
	relayClient := twitchClientForRestart(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
			return restartTextResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
		case req.Host == "api.twitch.tv" && req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
			return restartTextResponse(http.StatusOK, `{"data":[],"pagination":{},"total":0,"total_cost":0,"max_total_cost":10000}`), nil
		case req.Host == "api.twitch.tv" && req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
			var body struct {
				Type      string `json:"type"`
				Version   string `json:"version"`
				Transport struct {
					Callback string `json:"callback"`
					Secret   string `json:"secret"`
				} `json:"transport"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if body.Transport.Secret != boot1.Env.HMACSecret {
				t.Fatalf("create secret = %q, want configured secret", body.Transport.Secret)
			}
			created[body.Type] = body.Transport.Callback
			id := "restart-" + strings.ReplaceAll(body.Type, ".", "-")
			return restartTextResponse(http.StatusAccepted, restartEventSubCreateResponse(id, body.Type, body.Version, broadcasterID, body.Transport.Callback)), nil
		default:
			t.Fatalf("unexpected Twitch request during relay restart: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})
	relayRuntime := eventsubsvc.New(repo, relayClient, boot1.ServerModeCallbackURL(), boot1.Env.HMACSecret, log)
	if err := relayRuntime.ReconcileChannelSubs(ctx, map[string]bool{broadcasterID: true}); err != nil {
		t.Fatalf("relay restart reconcile error = %v, want nil", err)
	}
	for _, typ := range []string{"stream.online", "stream.offline"} {
		if created[typ] != relayIngest {
			t.Fatalf("%s created callback = %q, want %q", typ, created[typ], relayIngest)
		}
	}

	relaySettingsSvc := New(repo, boot1, log)
	savedPoll, err := relaySettingsSvc.Update(ctx, UpdateInput{
		Mode: config.ServerModePoll,
	})
	if err != nil {
		t.Fatalf("Update(poll) error = %v, want nil", err)
	}
	if !savedPoll.RestartRequired {
		t.Fatal("saving poll over active relay must require restart")
	}
	activeBeforeRestart, err := repo.ListActiveSubscriptions(ctx, 100, 0)
	if err != nil {
		t.Fatalf("ListActiveSubscriptions before cleanup: %v", err)
	}
	if len(activeBeforeRestart) != 2 {
		t.Fatalf("active subscriptions before poll restart = %d, want 2", len(activeBeforeRestart))
	}

	boot2 := restartConfig(config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset})
	resolved, err = Resolve(ctx, repo, boot2)
	if err != nil {
		t.Fatalf("Resolve() after poll save error = %v, want nil", err)
	}
	boot2.ServerMode = resolved
	if boot2.ServerMode.Mode != config.ServerModePoll {
		t.Fatalf("restart resolved mode = %q, want poll", boot2.ServerMode.Mode)
	}

	deleted := map[string]bool{}
	cleanupClient := twitchClientForRestart(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
			return restartTextResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
		case req.Host == "api.twitch.tv" && req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
			deleted[req.URL.Query().Get("id")] = true
			return restartTextResponse(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected Twitch request during cleanup restart: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})
	cleanupRuntime := eventsubsvc.New(repo, cleanupClient, boot2.ServerModeCallbackURL(), boot2.Env.HMACSecret, log)
	if err := CleanupNonSubscriptionRuntime(ctx, boot2.ServerMode, cleanupRuntime, log); err != nil {
		t.Fatalf("CleanupNonSubscriptionRuntime() error = %v, want nil", err)
	}
	for _, id := range []string{"restart-stream-online", "restart-stream-offline"} {
		if !deleted[id] {
			t.Fatalf("Twitch DELETE was not called for %s", id)
		}
	}
	activeAfterCleanup, err := repo.ListActiveSubscriptions(ctx, 100, 0)
	if err != nil {
		t.Fatalf("ListActiveSubscriptions after cleanup: %v", err)
	}
	if len(activeAfterCleanup) != 0 {
		t.Fatalf("active subscriptions after poll restart cleanup = %d, want 0", len(activeAfterCleanup))
	}
}

func restartConfig(mode config.ServerModeConfig) *config.Config {
	return &config.Config{
		App: config.AppConfig{
			Development: true,
		},
		Env: config.Environment{
			HMACSecret: "0123456789abcdef",
			Port:       8080,
		},
		ServerMode: mode,
	}
}

func seedRestartChannel(t *testing.T, ctx context.Context, repo repository.Repository, broadcasterID string) {
	t.Helper()
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID:          broadcasterID,
		Login:       broadcasterID,
		DisplayName: broadcasterID,
		Role:        "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: broadcasterID,
		BroadcasterName:  broadcasterID,
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

func twitchClientForRestart(t *testing.T, fn func(*http.Request) (*http.Response, error)) *twitch.Client {
	t.Helper()
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{Transport: restartRoundTripFunc(fn)})
	return tc
}

func restartTextResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func restartEventSubCreateResponse(id, typ, version, broadcasterID, callbackURL string) string {
	return fmt.Sprintf(`{"data":[{"id":%q,"status":"enabled","type":%q,"version":%q,"condition":{"broadcaster_user_id":%q},"created_at":%q,"transport":{"method":"webhook","callback":%q},"cost":1}]}`,
		id, typ, version, broadcasterID, time.Now().UTC().Format(time.RFC3339Nano), callbackURL)
}
