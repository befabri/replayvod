package igdb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeTokenProvider struct {
	token string
	err   error
	calls int
}

func (f *fakeTokenProvider) AppAccessToken(context.Context) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

func TestGetGames_PostsAPICalypseQueryWithTwitchAuth(t *testing.T) {
	tokenProvider := &fakeTokenProvider{token: "app-token"}
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/games" {
			t.Fatalf("path = %s, want /games", r.URL.Path)
		}
		if got := r.Header.Get("Client-ID"); got != "client-id" {
			t.Fatalf("Client-ID = %q, want client-id", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer app-token" {
			t.Fatalf("Authorization = %q, want Bearer app-token", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":42,"name":"Game","summary":"Summary","storyline":"Story","url":"https://igdb.com/game"}]`))
	}))
	defer server.Close()

	client := NewClient("client-id", tokenProvider, nil)
	client.SetBaseURL(server.URL)
	games, err := client.GetGames(context.Background(), []int64{42, 42, 0, -1})
	if err != nil {
		t.Fatalf("GetGames: %v", err)
	}
	if tokenProvider.calls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenProvider.calls)
	}
	if !strings.Contains(gotBody, "fields id,name,summary,storyline,url;") {
		t.Fatalf("body missing fields clause: %q", gotBody)
	}
	if !strings.Contains(gotBody, "where id = (42);") {
		t.Fatalf("body missing sanitized id clause: %q", gotBody)
	}
	if !strings.Contains(gotBody, "limit 1;") {
		t.Fatalf("body missing limit: %q", gotBody)
	}
	if len(games) != 1 || games[0].ID != 42 || games[0].Summary != "Summary" {
		t.Fatalf("games = %+v, want parsed game", games)
	}
}

func TestGetGames_EmptyIDsIsNoOp(t *testing.T) {
	tokenProvider := &fakeTokenProvider{token: "app-token"}
	client := NewClient("client-id", tokenProvider, nil)
	games, err := client.GetGames(context.Background(), []int64{0, -1})
	if err != nil {
		t.Fatalf("GetGames: %v", err)
	}
	if len(games) != 0 {
		t.Fatalf("games = %+v, want empty", games)
	}
	if tokenProvider.calls != 0 {
		t.Fatalf("token calls = %d, want 0", tokenProvider.calls)
	}
}

func TestGetGames_RetriesRateLimit(t *testing.T) {
	tokenProvider := &fakeTokenProvider{token: "app-token"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[{"id":7,"summary":"ok"}]`))
	}))
	defer server.Close()

	client := NewClient("client-id", tokenProvider, nil)
	client.SetBaseURL(server.URL)
	client.SetRetryBaseDelay(0)
	games, err := client.GetGames(context.Background(), []int64{7})
	if err != nil {
		t.Fatalf("GetGames: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want retry once", requests)
	}
	if len(games) != 1 || games[0].ID != 7 {
		t.Fatalf("games = %+v, want retried response", games)
	}
}

func TestGetGames_NonRetryableStatusReturnsError(t *testing.T) {
	tokenProvider := &fakeTokenProvider{token: "app-token"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad query", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient("client-id", tokenProvider, nil)
	client.SetBaseURL(server.URL)
	_, err := client.GetGames(context.Background(), []int64{7})
	var igdbErr *Error
	if !errors.As(err, &igdbErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if igdbErr.Status != http.StatusBadRequest || !strings.Contains(igdbErr.Body, "bad query") {
		t.Fatalf("IGDB error = %+v, want 400 bad query", igdbErr)
	}
}

func TestGetGames_DecodeErrorDoesNotRetry(t *testing.T) {
	tokenProvider := &fakeTokenProvider{token: "app-token"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := NewClient("client-id", tokenProvider, nil)
	client.SetBaseURL(server.URL)
	client.SetRetryBaseDelay(0)
	_, err := client.GetGames(context.Background(), []int64{7})
	if err == nil {
		t.Fatal("GetGames error = nil, want decode error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want no retry on decode error", requests)
	}
}

func TestGetGames_TokenErrorDoesNotRetryAPI(t *testing.T) {
	sentinel := errors.New("token failed")
	tokenProvider := &fakeTokenProvider{err: sentinel}
	client := NewClient("client-id", tokenProvider, nil)
	_, err := client.GetGames(context.Background(), []int64{1})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want token sentinel", err)
	}
	if tokenProvider.calls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenProvider.calls)
	}
}
