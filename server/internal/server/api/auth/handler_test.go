package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

func testAuthConfig() *config.Config {
	return &config.Config{
		Env: config.Environment{
			Host:        "localhost",
			CallbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
			FrontendURL: "http://localhost:3000",
		},
	}
}

func TestHandleRedirect_InviteCookieFlags(t *testing.T) {
	for _, host := range []string{"localhost", "0.0.0.0", "replay.example"} {
		t.Run(host, func(t *testing.T) {
			cfg := testAuthConfig()
			cfg.Env.Host = host
			h := NewHandler(cfg, twitch.NewClient("client-id", "secret", discardLog()), nil, nil, discardLog())
			rr := httptest.NewRecorder()
			h.handleRedirect(rr, httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch?invite=raw-token", nil))
			if rr.Code != http.StatusTemporaryRedirect {
				t.Fatalf("redirect status = %d", rr.Code)
			}
			cookies := rr.Result().Cookies()
			for _, name := range []string{stateCookieName, verifierCookieName, inviteCookieName} {
				c := cookieByName(cookies, name)
				if c == nil || c.Value == "" || c.Path != "/" || c.MaxAge != 300 || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure != (host == "replay.example") {
					t.Fatalf("OAuth cookie flags for %s: %+v", name, c)
				}
			}
			authorize, err := url.Parse(rr.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if authorize.Query().Get("invite") != "" || authorize.Query().Get("state") != cookieByName(cookies, stateCookieName).Value {
				t.Fatalf("invite leaked to provider or state not bound: %s", authorize)
			}
		})
	}
}

func TestHandleCallback_InvalidOAuthDoesNotConsumeInvite(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	raw := seedInvite(t, repo, "admin", time.Hour)
	// Nil dependencies make an unexpected code exchange or session creation
	// fail immediately.
	h := NewHandler(testAuthConfig(), nil, nil, nil, discardLog())
	for _, tc := range []struct {
		name          string
		query         string
		state         string
		verifier      string
		status        int
		clearsCookies bool
	}{
		{"missing state cookie", "state=valid&code=code", "", "verifier", http.StatusBadRequest, false},
		{"missing query state", "code=code", "valid", "verifier", http.StatusBadRequest, false},
		{"mismatched state", "state=wrong&code=code", "valid", "verifier", http.StatusBadRequest, false},
		{"missing verifier", "state=valid&code=code", "valid", "", http.StatusBadRequest, false},
		{"missing code", "state=valid", "valid", "verifier", http.StatusBadRequest, true},
		{"provider denial", "state=valid&error=access_denied", "valid", "verifier", http.StatusTemporaryRedirect, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?"+tc.query, nil)
			req.AddCookie(&http.Cookie{Name: inviteCookieName, Value: raw})
			if tc.state != "" {
				req.AddCookie(&http.Cookie{Name: stateCookieName, Value: tc.state})
			}
			if tc.verifier != "" {
				req.AddCookie(&http.Cookie{Name: verifierCookieName, Value: tc.verifier})
			}
			rr := httptest.NewRecorder()
			h.handleCallback(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rr.Code, tc.status, rr.Body.String())
			}
			if tc.status == http.StatusTemporaryRedirect && rr.Header().Get("Location") != "http://localhost:3000/login?error=access_denied" {
				t.Fatalf("denial redirect = %q", rr.Header().Get("Location"))
			}
			if tc.clearsCookies {
				for _, name := range []string{stateCookieName, verifierCookieName, inviteCookieName} {
					c := cookieByName(rr.Result().Cookies(), name)
					if c == nil || c.MaxAge >= 0 || c.Value != "" || c.Path != "/" {
						t.Fatalf("failed OAuth left cookie %s: %+v", name, c)
					}
				}
			}
			if c := cookieByName(rr.Result().Cookies(), session.CookieName); c != nil {
				t.Fatalf("failed OAuth created session cookie: %+v", c)
			}
			inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
			if err != nil || inv.RedeemedAt != nil || inv.RedeemedBy != nil {
				t.Fatalf("failed OAuth consumed invite: %+v, %v", inv, err)
			}
		})
	}
}

func cookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// TestHandleRedirect_StashesInviteCookie checks that ordinary login clears
// abandoned invite credentials.
func TestHandleRedirect_StashesInviteCookie(t *testing.T) {
	h := NewHandler(testAuthConfig(), twitch.NewClient("client-id", "secret", discardLog()), nil, nil, discardLog())

	rr := httptest.NewRecorder()
	h.handleRedirect(rr, httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch?invite=raw-tok-1", nil))
	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", rr.Code)
	}
	c := cookieByName(rr.Result().Cookies(), inviteCookieName)
	if c == nil {
		t.Fatal("invite cookie not set")
	}
	if c.Value != "raw-tok-1" || !c.HttpOnly || c.MaxAge != 300 {
		t.Fatalf("invite cookie = %+v, want raw-tok-1 / HttpOnly / MaxAge 300", c)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch", nil)
	req.AddCookie(&http.Cookie{Name: inviteCookieName, Value: "raw-tok-1"})
	h.handleRedirect(rr, req)
	c = cookieByName(rr.Result().Cookies(), inviteCookieName)
	if c == nil {
		t.Fatal("ordinary login must clear a lingering invite cookie")
	}
	if c.Value != "" || c.MaxAge >= 0 {
		t.Fatalf("invite cookie not expired on ordinary login: %+v", c)
	}
}

func TestHandleCallback_RedeemsInviteFromCookie(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := discardLog()
	sessionMgr, err := session.NewManager(repo, "auth-handler-test-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	tc := newStubbedTwitch(t, stubUserJSON)
	svc := New(repo, sessionMgr, tc, Config{WhitelistEnabled: true}, log)
	h := NewHandler(testAuthConfig(), tc, sessionMgr, svc, log)

	raw := seedInvite(t, repo, "admin", time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=state-1&code=code-1", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "state-1"})
	req.AddCookie(&http.Cookie{Name: verifierCookieName, Value: "verifier-1"})
	req.AddCookie(&http.Cookie{Name: inviteCookieName, Value: raw})
	rr := httptest.NewRecorder()
	h.handleCallback(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307 (body: %s)", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "http://localhost:3000/dashboard" {
		t.Fatalf("redirect = %q, want the dashboard", loc)
	}

	user, err := repo.GetUser(ctx, "twitch-1")
	if err != nil {
		t.Fatalf("invited user not created: %v", err)
	}
	if user.Role != "admin" {
		t.Fatalf("invited user role = %q, want admin", user.Role)
	}
	inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
	if err != nil {
		t.Fatalf("reload invite: %v", err)
	}
	if inv.RedeemedAt == nil || inv.RedeemedBy == nil || *inv.RedeemedBy != "twitch-1" {
		t.Fatalf("invite not consumed: %+v", inv)
	}

	cookies := rr.Result().Cookies()
	for _, name := range []string{stateCookieName, verifierCookieName, inviteCookieName} {
		c := cookieByName(cookies, name)
		if c == nil || c.MaxAge >= 0 || c.Value != "" {
			t.Fatalf("cookie %s not cleared: %+v", name, c)
		}
	}
	if c := cookieByName(cookies, session.CookieName); c == nil || c.Value == "" {
		t.Fatal("session cookie not set after invite redemption")
	}

	sessions, err := repo.ListUserSessions(ctx, "twitch-1")
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %d, %v; want 1", len(sessions), err)
	}
}

func TestHandleCallback_InvalidInviteRedirectsToLoginError(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := discardLog()
	sessionMgr, err := session.NewManager(repo, "auth-handler-test-session-secret-9876543210", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	tc := newStubbedTwitch(t, stubUserJSON)
	svc := New(repo, sessionMgr, tc, Config{}, log)
	h := NewHandler(testAuthConfig(), tc, sessionMgr, svc, log)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=state-1&code=code-1", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "state-1"})
	req.AddCookie(&http.Cookie{Name: verifierCookieName, Value: "verifier-1"})
	req.AddCookie(&http.Cookie{Name: inviteCookieName, Value: "stale-token"})
	rr := httptest.NewRecorder()
	h.handleCallback(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "http://localhost:3000/login?error=invite_invalid" {
		t.Fatalf("redirect = %q, want /login?error=invite_invalid", loc)
	}
	if _, err := repo.GetUser(ctx, "twitch-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("denied redemption must not persist a user, GetUser err = %v", err)
	}
	for _, name := range []string{stateCookieName, verifierCookieName, inviteCookieName} {
		c := cookieByName(rr.Result().Cookies(), name)
		if c == nil || c.MaxAge >= 0 || c.Value != "" {
			t.Fatalf("denied redemption left cookie %s: %+v", name, c)
		}
	}
	if c := cookieByName(rr.Result().Cookies(), session.CookieName); c != nil {
		t.Fatalf("denied redemption set a session cookie: %+v", c)
	}
	if sessions, err := repo.ListUserSessions(ctx, "twitch-1"); err != nil || len(sessions) != 0 {
		t.Fatalf("denied redemption created sessions: %+v, %v", sessions, err)
	}
}
