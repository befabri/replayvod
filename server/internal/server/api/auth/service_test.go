package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newStubbedTwitch fakes OAuth token exchange and GET /users.
func newStubbedTwitch(t *testing.T, usersJSON string) *twitch.Client {
	t.Helper()
	tc := twitch.NewClient("client-id", "secret", discardLog())
	tc.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch {
		case r.URL.Host == "id.twitch.tv":
			body = `{"access_token":"access-tok","refresh_token":"refresh-tok","expires_in":3600,"token_type":"bearer"}`
		case strings.HasSuffix(r.URL.Path, "/users"):
			body = usersJSON
		default:
			t.Errorf("unexpected twitch request: %s", r.URL.String())
			body = "{}"
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	return tc
}

const stubUserJSON = `{"data":[{"id":"twitch-1","login":"streamer","display_name":"Streamer","email":"s@example.com"}]}`

func TestHandleOAuthCallback_FirstUserBecomesOwnerAndPersists(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier")
	if err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if res.User.ID != "twitch-1" || res.User.Login != "streamer" {
		t.Fatalf("user = %+v, want id twitch-1 / login streamer", res.User)
	}
	if res.User.Role != "owner" {
		t.Fatalf("first-ever user role = %q, want owner", res.User.Role)
	}
	if res.Tokens.AccessToken != "access-tok" || res.Tokens.RefreshToken != "refresh-tok" {
		t.Fatalf("tokens = %+v, want access-tok/refresh-tok", res.Tokens)
	}
	stored, err := repo.GetUser(ctx, "twitch-1")
	if err != nil {
		t.Fatalf("user not persisted: %v", err)
	}
	if stored.Role != "owner" {
		t.Fatalf("persisted role = %q, want owner", stored.Role)
	}
}

func TestHandleOAuthCallback_PreservesExistingRole(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: "admin"}); err != nil {
		t.Fatalf("seed existing user: %v", err)
	}
	// Stored roles win over OwnerTwitchID recomputation.
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{OwnerTwitchID: "someone-else"}, discardLog())

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier")
	if err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if res.User.Role != "admin" {
		t.Fatalf("existing role overwritten: got %q, want admin", res.User.Role)
	}
}

func TestHandleOAuthCallback_WhitelistDenied(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{WhitelistEnabled: true}, discardLog())

	_, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier")
	var denied *ErrLoginDenied
	if !errors.As(err, &denied) {
		t.Fatalf("err = %v, want *ErrLoginDenied", err)
	}
	if denied.Reason != "not_whitelisted" {
		t.Fatalf("denied reason = %q, want not_whitelisted", denied.Reason)
	}
	if _, err := repo.GetUser(ctx, "twitch-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("denied login must not persist a user, GetUser err = %v", err)
	}
}

// fakeRoleRepo overrides only ListUsers; embedded methods panic if called.
type fakeRoleRepo struct {
	repository.Repository
	users []repository.User
	err   error
}

func (f fakeRoleRepo) ListUsers(context.Context) ([]repository.User, error) {
	return f.users, f.err
}

func TestResolveRole(t *testing.T) {
	oneUser := []repository.User{{}}

	cases := []struct {
		name          string
		ownerTwitchID string
		twitchID      string
		users         []repository.User
		listErr       error
		want          string
	}{
		{"configured owner matches → owner", "owner-123", "owner-123", oneUser, nil, "owner"},
		{"configured owner, other user, users exist → viewer", "owner-123", "rando-456", oneUser, nil, "viewer"},
		{"no owner configured, first-ever user → owner", "", "first-1", nil, nil, "owner"},
		{"no owner configured, later user → viewer", "", "later-2", oneUser, nil, "viewer"},
		// Bootstrap still wins before the configured owner has logged in.
		{"configured owner absent, other first user still bootstraps → owner", "owner-123", "rando-456", nil, nil, "owner"},
		// Do not treat ListUsers failure as an empty user table.
		{"ListUsers error → viewer (not owner)", "", "x", nil, errors.New("db down"), "viewer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{
				repo: fakeRoleRepo{users: tc.users, err: tc.listErr},
				cfg:  Config{OwnerTwitchID: tc.ownerTwitchID},
				log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			if got := s.resolveRole(context.Background(), tc.twitchID); got != tc.want {
				t.Fatalf("resolveRole(%q) = %q, want %q", tc.twitchID, got, tc.want)
			}
		})
	}
}
