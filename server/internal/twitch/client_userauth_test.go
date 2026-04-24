package twitch

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type fakeUserTokenProvider struct {
	mu         sync.Mutex
	forced     int
	normal     int
	firstToken string
	newToken   string
}

func (f *fakeUserTokenProvider) AccessToken(_ context.Context, force bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if force {
		f.forced++
		return f.newToken, nil
	}
	f.normal++
	return f.firstToken, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientRetriesWithForcedUserRefresh(t *testing.T) {
	provider := &fakeUserTokenProvider{firstToken: "stale-token", newToken: "fresh-token"}
	var seen []string
	client := NewClient("client-id", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.Header.Get("Authorization"))
		body := `{"error":"Unauthorized","status":401,"message":"Invalid OAuth token"}`
		status := http.StatusUnauthorized
		if r.Header.Get("Authorization") == "Bearer fresh-token" {
			body = `{"data":[{"id":"1","login":"login","display_name":"Name"}]}`
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	ctx := WithUserTokenProvider(context.Background(), provider)
	users, err := client.GetUsers(ctx, &GetUsersParams{ID: []string{"1"}})
	if err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
	if len(users) != 1 || users[0].ID != "1" {
		t.Fatalf("users = %#v, want single user", users)
	}
	if provider.normal != 1 {
		t.Fatalf("normal token calls = %d, want 1", provider.normal)
	}
	if provider.forced != 1 {
		t.Fatalf("forced token calls = %d, want 1", provider.forced)
	}
	if len(seen) != 2 || seen[0] != "Bearer stale-token" || seen[1] != "Bearer fresh-token" {
		t.Fatalf("authorization headers = %#v, want stale then fresh", seen)
	}
}

func TestClientPrefersManagedProviderOverDirectUserToken(t *testing.T) {
	provider := &fakeUserTokenProvider{firstToken: "managed-token", newToken: "managed-fresh"}
	var seen string
	client := NewClient("client-id", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"1","login":"login","display_name":"Name"}]}`)),
		}, nil
	})}

	ctx := WithUserToken(context.Background(), "direct-token")
	ctx = WithUserTokenProvider(ctx, provider)
	if _, err := client.GetUsers(ctx, &GetUsersParams{ID: []string{"1"}}); err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
	if seen != "Bearer managed-token" {
		t.Fatalf("authorization header = %q, want managed token", seen)
	}
}
