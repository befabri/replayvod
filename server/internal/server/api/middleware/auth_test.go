package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/trpcgo"
)

type authRepo struct {
	repository.Repository
	sess                *repository.Session
	user                *repository.User
	sessionErr, userErr error
	activity            int
	deleted             string
}

func (r *authRepo) CreateSession(_ context.Context, sess *repository.Session) error {
	r.sess = sess
	return nil
}

func (r *authRepo) GetSession(_ context.Context, hash string) (*repository.Session, error) {
	if hash != r.sess.HashedID {
		return nil, repository.ErrNotFound
	}
	return r.sess, r.sessionErr
}

func (r *authRepo) GetUser(_ context.Context, _ string) (*repository.User, error) {
	return r.user, r.userErr
}

func (r *authRepo) UpdateSessionActivity(_ context.Context, _ string) error {
	r.activity++
	return nil
}

func (r *authRepo) DeleteSession(_ context.Context, hash string) error {
	r.deleted = hash
	return nil
}

func TestAuthenticationTransports(t *testing.T) {
	outage := errors.New("database connection failed: private diagnostic")
	for _, tc := range []struct {
		name                       string
		sessionErr, userErr        error
		noCookie, expired, corrupt bool
		status                     int
	}{
		{name: "authenticated", status: http.StatusOK},
		{name: "no cookie", noCookie: true, status: http.StatusUnauthorized},
		{name: "revoked", sessionErr: fmt.Errorf("lookup: %w", repository.ErrNotFound), status: http.StatusUnauthorized},
		{name: "expired", expired: true, status: http.StatusUnauthorized},
		{name: "deleted user", userErr: fmt.Errorf("lookup: %w", repository.ErrNotFound), status: http.StatusUnauthorized},
		{name: "session outage", sessionErr: outage, status: http.StatusInternalServerError},
		{name: "user outage", userErr: outage, status: http.StatusInternalServerError},
		{name: "canceled lookup", sessionErr: context.Canceled, status: statusClientClosed},
		{name: "wrapped canceled lookup", sessionErr: fmt.Errorf("query: %w", context.Canceled), status: statusClientClosed},
		{name: "canceled user lookup", userErr: fmt.Errorf("query: %w", context.Canceled), status: statusClientClosed},
		{name: "lookup deadline", sessionErr: context.DeadlineExceeded, status: http.StatusRequestTimeout},
		{name: "wrapped lookup deadline", sessionErr: fmt.Errorf("query: %w", context.DeadlineExceeded), status: http.StatusRequestTimeout},
		{name: "user lookup deadline", userErr: fmt.Errorf("query: %w", context.DeadlineExceeded), status: http.StatusRequestTimeout},
		{name: "unreadable stored tokens", corrupt: true, status: http.StatusUnauthorized},
	} {
		for _, transport := range []string{"http", "trpc"} {
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				var logs bytes.Buffer
				log := slog.New(slog.NewTextHandler(&logs, nil))
				repo := &authRepo{user: &repository.User{ID: "user", Role: RoleOwner}, sessionErr: tc.sessionErr, userErr: tc.userErr}
				mgr, err := session.NewManager(repo, "0123456789abcdef0123456789abcdef", false, log)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				created := httptest.NewRecorder()
				tokens := &session.TwitchTokens{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().UTC().Add(time.Hour)}
				if err := mgr.Create(r.Context(), created, repo.user.ID, tokens, r); err != nil {
					t.Fatal(err)
				}
				if !tc.noCookie {
					r.AddCookie(created.Result().Cookies()[0])
				}
				if tc.expired {
					repo.sess.ExpiresAt = time.Now().Add(-time.Hour)
				}
				if tc.corrupt {
					repo.sess.EncryptedTokens = []byte("corrupt ciphertext")
				}
				if tc.status == statusClientClosed {
					ctx, cancel := context.WithCancel(r.Context())
					cancel()
					r = r.WithContext(ctx)
				}
				if tc.status == http.StatusRequestTimeout {
					ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(-time.Second))
					defer cancel()
					r = r.WithContext(ctx)
				}
				auth := NewAuthenticator(mgr, repo, nil, log)
				called := false
				checkContext := func(ctx context.Context) {
					t.Helper()
					called = true
					gotTokens := GetTokens(ctx)
					if GetUser(ctx) != repo.user || GetSession(ctx) != repo.sess || gotTokens == nil || gotTokens.AccessToken != tokens.AccessToken {
						t.Fatalf("authenticated context missing user, session or tokens")
					}
				}
				if transport == "http" {
					w := httptest.NewRecorder()
					auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						checkContext(r.Context())
						w.WriteHeader(http.StatusOK)
					})).ServeHTTP(w, r)
					if w.Code != tc.status {
						t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.status, w.Body)
					}
					if len(w.Result().Cookies()) != 0 {
						t.Fatal("authentication response changed the session cookie")
					}
					if strings.Contains(w.Body.String(), outage.Error()) {
						t.Fatal("response leaked database diagnostics")
					}
				} else {
					_, err := auth.TRPC(func(ctx context.Context, input any) (any, error) {
						checkContext(ctx)
						return nil, nil
					})(WithContextCreator(r.Context(), r), nil)
					if tc.status == http.StatusOK {
						if err != nil {
							t.Fatal(err)
						}
					} else {
						var rpcErr *trpcgo.Error
						wantCode := map[int]trpcgo.ErrorCode{
							http.StatusUnauthorized:        trpcgo.CodeUnauthorized,
							http.StatusInternalServerError: trpcgo.CodeInternalServerError,
							statusClientClosed:             trpcgo.CodeClientClosed,
							http.StatusRequestTimeout:      trpcgo.CodeTimeout,
						}[tc.status]
						if !errors.As(err, &rpcErr) || rpcErr.Code != wantCode {
							t.Fatalf("error = %v, want code %v", err, wantCode)
						}
						if strings.Contains(rpcErr.Message, outage.Error()) {
							t.Fatal("response leaked database diagnostics")
						}
					}
				}
				if called != (tc.status == http.StatusOK) {
					t.Fatalf("next handler called = %v", called)
				}
				if (repo.activity == 1) != called {
					t.Fatalf("activity updates = %d; handler called = %v", repo.activity, called)
				}
				wantDeleted := tc.expired || tc.corrupt
				if (repo.deleted != "") != wantDeleted || wantDeleted && repo.deleted != repo.sess.HashedID {
					t.Fatalf("deleted session = %q; want revoked = %v", repo.deleted, wantDeleted)
				}
				if tc.corrupt && !strings.Contains(logs.String(), "unreadable tokens") {
					t.Fatal("revoked session was not logged")
				}
				if tc.status == http.StatusInternalServerError && !strings.Contains(logs.String(), "level=ERROR") {
					t.Fatal("operational failure was not logged")
				}
				if tc.status == statusClientClosed && logs.Len() != 0 {
					t.Fatalf("cancellation was logged: %s", logs.String())
				}
				if tc.status == http.StatusRequestTimeout {
					if strings.Contains(logs.String(), "authentication failed") {
						t.Fatalf("deadline was logged as an internal failure: %s", logs.String())
					}
					if transport == "http" && !strings.Contains(logs.String(), "authentication timed out") {
						t.Fatal("HTTP deadline was not logged as a timeout")
					}
					// tRPC deadlines are logged by the router's WithOnError hook.
				}
				if (tc.sessionErr == outage || tc.userErr == outage) && !strings.Contains(logs.String(), outage.Error()) {
					t.Fatal("database diagnostics were not retained in server logs")
				}
			})
		}
	}
}
