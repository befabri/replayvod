package video

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type fakeDownloadRepo struct {
	channel *repository.Channel
}

func (f *fakeDownloadRepo) GetChannel(_ context.Context, broadcasterID string) (*repository.Channel, error) {
	if f.channel == nil || f.channel.BroadcasterID != broadcasterID {
		return nil, repository.ErrNotFound
	}
	return f.channel, nil
}

func (f *fakeDownloadRepo) GetVideoByJobID(context.Context, string) (*repository.Video, error) {
	return nil, repository.ErrNotFound
}

type fakeDownloadRunner struct {
	params downloader.Params
	jobID  string
}

func (f *fakeDownloadRunner) Start(_ context.Context, p downloader.Params) (string, error) {
	f.params = p
	if f.jobID == "" {
		f.jobID = "job-1"
	}
	return f.jobID, nil
}

func (f *fakeDownloadRunner) Cancel(string)                                   {}
func (f *fakeDownloadRunner) Subscribe(string) <-chan downloader.Progress     { return nil }
func (f *fakeDownloadRunner) ListActiveProgress() []downloader.Progress       { return nil }
func (f *fakeDownloadRunner) SubscribeActive(context.Context) <-chan struct{} { return nil }

type authHydrator struct{ client *twitch.Client }

func (h authHydrator) Hydrate(ctx context.Context, broadcasterID string) *streammeta.Snapshot {
	streams, _, err := h.client.GetStreams(ctx, &twitch.GetStreamsParams{UserID: []string{broadcasterID}, First: 1})
	if err != nil || len(streams) == 0 {
		return nil
	}
	st := streams[0]
	return &streammeta.Snapshot{
		StreamID:    st.ID,
		Title:       st.Title,
		Language:    st.Language,
		ViewerCount: int64(st.ViewerCount),
		GameID:      st.GameID,
		GameName:    st.GameName,
		CategoryIDs: nil,
		TagIDs:      nil,
		StartedAt:   st.StartedAt,
	}
}

func TestTriggerUsesManagedProviderRetry(t *testing.T) {
	updater := &fakeSessionUpdater{}
	var streamCalls int
	client := twitch.NewClient("client-id", "secret", testClientLogger())
	client.SetHTTPClient(&http.Client{Transport: providerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "api.twitch.tv" && r.URL.Path == "/helix/streams":
			streamCalls++
			status := http.StatusUnauthorized
			body := `{"error":"Unauthorized","status":401,"message":"Invalid OAuth token"}`
			if r.Header.Get("Authorization") == "Bearer fresh-token" {
				status = http.StatusOK
				body = `{"data":[{"id":"stream-1","user_id":"b1","user_login":"b-login","user_name":"Broadcaster","type":"live","title":"Live Title","viewer_count":123,"started_at":"2026-04-23T12:00:00Z","language":"en","thumbnail_url":"https://img/thumb-{width}x{height}.jpg","game_id":"g1","game_name":"Game"}],"pagination":{}}`
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

	repo := &fakeDownloadRepo{channel: &repository.Channel{BroadcasterID: "b1", BroadcasterLogin: "b-login", BroadcasterName: "Broadcaster"}}
	runner := &fakeDownloadRunner{jobID: "job-1"}
	svc := &DownloadService{repo: repo, downloader: runner, twitch: client, hydrator: authHydrator{client: client}, log: testClientLogger()}

	got, err := svc.Trigger(ctx, TriggerInput{BroadcasterID: "b1", UserID: "user-1"})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got.JobID != "job-1" {
		t.Fatalf("job id = %q, want job-1", got.JobID)
	}
	if runner.params.Title != "Live Title" || runner.params.CategoryID != "g1" || runner.params.CategoryName != "Game" {
		t.Fatalf("downloader params = %#v, want hydrated metadata", runner.params)
	}
	if streamCalls != 2 {
		t.Fatalf("stream calls = %d, want 2", streamCalls)
	}
	if updater.updates != 1 {
		t.Fatalf("session token updates = %d, want 1", updater.updates)
	}
}
