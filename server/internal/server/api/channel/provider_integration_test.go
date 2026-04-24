package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type fakeChannelRepo struct {
	channel *repository.Channel
}

func (f *fakeChannelRepo) GetChannel(context.Context, string) (*repository.Channel, error) {
	return f.channel, nil
}
func (f *fakeChannelRepo) GetChannelByLogin(context.Context, string) (*repository.Channel, error) {
	return f.channel, nil
}
func (f *fakeChannelRepo) ListChannels(context.Context) ([]repository.Channel, error) {
	return nil, nil
}
func (f *fakeChannelRepo) ListChannelsPage(context.Context, int, string, bool, *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	return &repository.ChannelPage{}, nil
}
func (f *fakeChannelRepo) ListUserFollows(context.Context, string) ([]repository.Channel, error) {
	return nil, nil
}
func (f *fakeChannelRepo) SearchChannels(context.Context, string, int) ([]repository.Channel, error) {
	return nil, nil
}
func (f *fakeChannelRepo) ListLatestLivePerChannel(context.Context, int) ([]repository.LatestLiveStream, error) {
	return nil, nil
}
func (f *fakeChannelRepo) UpsertChannel(_ context.Context, c *repository.Channel) (*repository.Channel, error) {
	copy := *c
	f.channel = &copy
	return &copy, nil
}

func TestSyncFromTwitchUsesManagedProviderRetry(t *testing.T) {
	updater := &fakeSessionUpdater{}
	var userCalls int
	client := twitch.NewClient("client-id", "secret", testClientLogger())
	client.SetHTTPClient(&http.Client{Transport: providerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "api.twitch.tv" && r.URL.Path == "/helix/users":
			userCalls++
			status := http.StatusUnauthorized
			body := `{"error":"Unauthorized","status":401,"message":"Invalid OAuth token"}`
			if r.Header.Get("Authorization") == "Bearer fresh-token" {
				status = http.StatusOK
				body = `{"data":[{"id":"b1","login":"b-login","display_name":"Broadcaster","profile_image_url":"https://img/profile.png","offline_image_url":"https://img/offline.png","description":"desc","broadcaster_type":"partner"}]}`
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		case r.URL.Host == "api.twitch.tv" && r.URL.Path == "/helix/channels":
			body := `{"data":[{"broadcaster_id":"b1","broadcaster_language":"en"}]}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
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

	repo := &fakeChannelRepo{}
	svc := New(repo, client, testClientLogger())
	got, err := svc.SyncFromTwitch(ctx, SyncInput{BroadcasterID: "b1", UserID: "user-1"})
	if err != nil {
		t.Fatalf("SyncFromTwitch: %v", err)
	}
	if got == nil || got.BroadcasterID != "b1" || got.BroadcasterLanguage == nil || *got.BroadcasterLanguage != "en" {
		t.Fatalf("channel = %#v, want synced broadcaster", got)
	}
	if userCalls != 2 {
		t.Fatalf("user calls = %d, want 2", userCalls)
	}
	if updater.updates != 1 {
		t.Fatalf("session token updates = %d, want 1", updater.updates)
	}
}
