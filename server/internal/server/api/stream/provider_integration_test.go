package stream

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type providerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f providerRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeStreamRepo struct{}

func (fakeStreamRepo) ListActiveStreams(context.Context) ([]repository.Stream, error) {
	return nil, nil
}
func (fakeStreamRepo) ListStreamsByBroadcaster(context.Context, string, int, int) ([]repository.Stream, error) {
	return nil, nil
}
func (fakeStreamRepo) GetLastLiveStream(context.Context, string) (*repository.Stream, error) {
	return nil, repository.ErrNotFound
}
func (fakeStreamRepo) ListChannelsByIDs(context.Context, []string) ([]repository.Channel, error) {
	return []repository.Channel{}, nil
}
func (fakeStreamRepo) UpsertChannel(context.Context, *repository.Channel) (*repository.Channel, error) {
	return nil, nil
}
func (fakeStreamRepo) GetStream(context.Context, string) (*repository.Stream, error) { return nil, repository.ErrNotFound }
func (fakeStreamRepo) UpsertStream(context.Context, *repository.StreamInput) (*repository.Stream, error) {
	return nil, nil
}

type fakeSessionUpdater struct {
	updates int
	last    *session.TwitchTokens
}

func (f *fakeSessionUpdater) UpdateTokens(_ context.Context, _ string, tokens *session.TwitchTokens) error {
	f.updates++
	copy := *tokens
	f.last = &copy
	return nil
}

func testClientLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFollowedUsesManagedProviderRetry(t *testing.T) {
	updater := &fakeSessionUpdater{}
	var followedCalls int
	client := twitch.NewClient("client-id", "secret", testClientLogger())
	client.SetHTTPClient(&http.Client{Transport: providerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "api.twitch.tv" && r.URL.Path == "/helix/streams/followed":
			followedCalls++
			status := http.StatusUnauthorized
			body := `{"error":"Unauthorized","status":401,"message":"Invalid OAuth token"}`
			if r.Header.Get("Authorization") == "Bearer fresh-token" {
				status = http.StatusOK
				body = `{"data":[],"pagination":{}}`
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		case r.URL.Host == "id.twitch.tv" && r.URL.Path == "/oauth2/token":
			body := `{"access_token":"fresh-token","refresh_token":"refresh-2","expires_in":3600,"scope":[],"token_type":"bearer"}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})})

	provider := middleware.NewSessionTokenProvider(updater, client, testClientLogger())
	ctx := provider.Bind(context.Background(), "sess-1", &session.TwitchTokens{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	})

	svc := New(fakeStreamRepo{}, client, testClientLogger())
	got, err := svc.Followed(ctx, FollowedInput{UserID: "user-1"})
	if err != nil {
		t.Fatalf("Followed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty followed result, got %#v", got)
	}
	if followedCalls != 2 {
		t.Fatalf("followed calls = %d, want 2", followedCalls)
	}
	if updater.updates != 1 {
		t.Fatalf("session token updates = %d, want 1", updater.updates)
	}
	if updater.last == nil || updater.last.AccessToken != "fresh-token" || updater.last.RefreshToken != "refresh-2" {
		t.Fatalf("updated tokens = %#v, want refreshed token set", updater.last)
	}
}
