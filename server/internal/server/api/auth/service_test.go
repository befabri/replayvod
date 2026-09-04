package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/invite"
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
		// An empty follow page stops the callback's background sync.
		case strings.HasSuffix(r.URL.Path, "/channels/followed"):
			body = `{"data":[],"pagination":{}}`
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

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", "")
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

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", "")
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

	_, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", "")
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

func seedInvite(t *testing.T, repo repository.Repository, role string, ttl time.Duration) string {
	t.Helper()
	ctx := context.Background()
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "inviter-1", Login: "inviter", DisplayName: "Inviter", Role: "owner"}); err != nil {
		t.Fatalf("seed inviter: %v", err)
	}
	raw, err := invite.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: invite.HashToken(raw), Role: role, CreatedBy: "inviter-1",
		ExpiresAt: time.Now().Add(ttl),
	}); err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	return raw
}

func TestHandleOAuthCallback_InviteBypassesWhitelistAndSetsRole(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	raw := seedInvite(t, repo, "admin", time.Hour)
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{WhitelistEnabled: true}, discardLog())

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	if err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if res.User.Role != "admin" {
		t.Fatalf("invited user role = %q, want admin (invite role wins over viewer default)", res.User.Role)
	}

	inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
	if err != nil {
		t.Fatalf("reload invite: %v", err)
	}
	if inv.RedeemedAt == nil || inv.RedeemedBy == nil || *inv.RedeemedBy != "twitch-1" {
		t.Fatalf("invite not consumed: %+v", inv)
	}

	_, err = s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	var denied *ErrLoginDenied
	if !errors.As(err, &denied) || denied.Reason != "invite_invalid" {
		t.Fatalf("reuse err = %v, want ErrLoginDenied invite_invalid", err)
	}
}

func TestHandleOAuthCallback_InviteExpiredOrUnknownDenied(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	raw := seedInvite(t, repo, "viewer", -time.Minute)
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	for name, token := range map[string]string{"expired": raw, "unknown": "no-such-token"} {
		_, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", token)
		var denied *ErrLoginDenied
		if !errors.As(err, &denied) || denied.Reason != "invite_invalid" {
			t.Fatalf("%s token err = %v, want ErrLoginDenied invite_invalid", name, err)
		}
	}
	if _, err := repo.GetUser(ctx, "twitch-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("denied redemption must not persist a user, GetUser err = %v", err)
	}
}

func TestHandleOAuthCallback_InviteSelfRedemptionDenied(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	// The stubbed Twitch user (twitch-1) is also the invite's creator.
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: "owner"}); err != nil {
		t.Fatalf("seed creator: %v", err)
	}
	raw, err := invite.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: invite.HashToken(raw), Role: "admin", CreatedBy: "twitch-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	_, err = s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	var denied *ErrLoginDenied
	if !errors.As(err, &denied) || denied.Reason != "invite_self" {
		t.Fatalf("self-redemption err = %v, want ErrLoginDenied invite_self", err)
	}

	inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
	if err != nil {
		t.Fatalf("reload invite: %v", err)
	}
	if inv.RedeemedAt != nil || inv.RedeemedBy != nil {
		t.Fatalf("invite must stay pending after self-redemption attempt: %+v", inv)
	}
}

func TestHandleOAuthCallback_InviteUpgradesExistingUser(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: "viewer"}); err != nil {
		t.Fatalf("seed existing viewer: %v", err)
	}
	raw := seedInvite(t, repo, "admin", time.Hour)
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	if err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if res.User.Role != "admin" {
		t.Fatalf("existing viewer role after admin invite = %q, want admin", res.User.Role)
	}
	stored, err := repo.GetUser(ctx, "twitch-1")
	if err != nil || stored.Role != "admin" {
		t.Fatalf("invite promotion was not persisted: %+v, %v", stored, err)
	}
	inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
	if err != nil {
		t.Fatalf("reload invite: %v", err)
	}
	if inv.RedeemedAt == nil || inv.RedeemedBy == nil || *inv.RedeemedBy != "twitch-1" {
		t.Fatalf("invite not consumed: %+v", inv)
	}
}

func TestHandleOAuthCallback_InviteNeverDemotesOwner(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: "owner"}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	raw := seedInvite(t, repo, "viewer", time.Hour)
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	if err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if res.User.Role != "owner" {
		t.Fatalf("owner role after invite = %q, want owner preserved", res.User.Role)
	}
}

// TestHandleOAuthCallback_InviteNeverDemotesExistingUser guards against
// invite-based role downgrades.
func TestHandleOAuthCallback_InviteNeverDemotesExistingUser(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: "admin"}); err != nil {
		t.Fatalf("seed existing admin: %v", err)
	}
	raw := seedInvite(t, repo, "viewer", time.Hour)
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	res, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	if err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if res.User.Role != "admin" {
		t.Fatalf("existing admin role after viewer invite = %q, want admin preserved", res.User.Role)
	}
	stored, err := repo.GetUser(ctx, "twitch-1")
	if err != nil || stored.Role != "admin" {
		t.Fatalf("persisted role = %q, %v; want admin", stored.Role, err)
	}
}

func TestHandleOAuthCallback_InvitePersistsWhitelistAccess(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	raw := seedInvite(t, repo, "viewer", time.Hour)
	s := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{WhitelistEnabled: true}, discardLog())

	if _, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw); err != nil {
		t.Fatalf("invite login: %v", err)
	}
	if ok, err := repo.IsWhitelisted(ctx, "twitch-1"); err != nil || !ok {
		t.Fatalf("IsWhitelisted after redemption = %v, %v; want true", ok, err)
	}
	if _, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", ""); err != nil {
		t.Fatalf("ordinary login after invite must pass the whitelist: %v", err)
	}
}

// raceLostRepo simulates a token claimed or expired before redemption.
type raceLostRepo struct {
	repository.Repository
}

type userLookupFailureRepo struct {
	repository.Repository
	err error
}

func (r userLookupFailureRepo) GetUser(context.Context, string) (*repository.User, error) {
	return nil, r.err
}

func (r userLookupFailureRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(userLookupFailureRepo{Repository: tx, err: r.err})
	})
}

func TestHandleOAuthCallback_UserLookupFailurePreservesRoleAndInvite(t *testing.T) {
	for _, withInvite := range []bool{false, true} {
		name := "ordinary login"
		if withInvite {
			name = "invited login"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: "admin"}); err != nil {
				t.Fatal(err)
			}
			raw := ""
			if withInvite {
				raw = seedInvite(t, repo, "viewer", time.Hour)
			}
			lookupErr := errors.New("user lookup unavailable")
			svc := New(userLookupFailureRepo{Repository: repo, err: lookupErr}, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())
			result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
			if result != nil || !errors.Is(err, lookupErr) {
				t.Errorf("login on failed user lookup = (%+v, %v), want no login and lookup error", result, err)
			}
			stored, err := repo.GetUser(ctx, "twitch-1")
			if err != nil || stored.Role != "admin" {
				t.Fatalf("failed lookup changed persisted role: %+v, %v", stored, err)
			}
			if withInvite {
				inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
				if err != nil || inv.RedeemedAt != nil || inv.RedeemedBy != nil {
					t.Fatalf("failed lookup consumed invite: %+v, %v", inv, err)
				}
			}
		})
	}
}

func (raceLostRepo) RedeemInvite(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r raceLostRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(raceLostRepo{tx})
	})
}

// TestHandleOAuthCallback_LostRedemptionRaceLeavesNoUser checks that failed
// claims cannot create accounts.
func TestHandleOAuthCallback_LostRedemptionRaceLeavesNoUser(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	raw := seedInvite(t, repo, "admin", time.Hour)
	s := New(raceLostRepo{repo}, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	_, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	var denied *ErrLoginDenied
	if !errors.As(err, &denied) || denied.Reason != "invite_invalid" {
		t.Fatalf("err = %v, want ErrLoginDenied invite_invalid", err)
	}
	if _, err := repo.GetUser(ctx, "twitch-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("lost redemption must not persist a user, GetUser err = %v", err)
	}
}

// TestHandleOAuthCallback_LostRedemptionRaceKeepsExistingRole checks that
// failed claims cannot grant roles.
func TestHandleOAuthCallback_LostRedemptionRaceKeepsExistingRole(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: "viewer"}); err != nil {
		t.Fatalf("seed existing viewer: %v", err)
	}
	raw := seedInvite(t, repo, "admin", time.Hour)
	s := New(raceLostRepo{repo}, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())

	_, err := s.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
	var denied *ErrLoginDenied
	if !errors.As(err, &denied) || denied.Reason != "invite_invalid" {
		t.Fatalf("err = %v, want ErrLoginDenied invite_invalid", err)
	}
	stored, err := repo.GetUser(ctx, "twitch-1")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.Role != "viewer" {
		t.Fatalf("role after lost redemption = %q, want viewer (no promotion)", stored.Role)
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
		// A failed bootstrap lookup must not permanently prevent
		// creation of the first owner.
		{"ListUsers error → no role", "", "x", nil, errors.New("db down"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{
				repo: fakeRoleRepo{users: tc.users, err: tc.listErr},
				cfg:  Config{OwnerTwitchID: tc.ownerTwitchID},
				log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			got, err := s.resolveRole(context.Background(), s.repo, tc.twitchID)
			if !errors.Is(err, tc.listErr) {
				t.Fatalf("resolveRole error = %v, want %v", err, tc.listErr)
			}
			if got != tc.want {
				t.Fatalf("resolveRole(%q) = %q, want %q", tc.twitchID, got, tc.want)
			}
		})
	}
}
