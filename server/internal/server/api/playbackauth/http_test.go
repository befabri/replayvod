package playbackauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	svc "github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/validate"
	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

type httpValidator struct {
	status atomic.Int32
	calls  atomic.Int32
}

func (v *httpValidator) Validate(context.Context, string) (svc.Identity, error) {
	v.calls.Add(1)
	switch v.status.Load() {
	case 401:
		return svc.Identity{}, svc.ErrRejected
	case 503:
		return svc.Identity{}, svc.ErrUnavailable
	}
	return svc.Identity{UserID: "123", Login: "viewer"}, nil
}

// TestPlaybackConnectionHTTPLifecycle covers the HTTP transport guard, the
// service and a real database. Role and CSRF enforcement are exercised by the
// full router's permission and origin matrices.
func TestPlaybackConnectionHTTPLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	v := &httpValidator{}
	h := &Handler{svc: svc.New(repo, strings.Repeat("s", 32), v), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	router := trpcgo.NewRouter(trpcgo.WithContextCreator(middleware.WithContextCreator), trpcgo.WithValidator(validate.V.Struct))
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	trpcgo.MustMutation(router, "twitchPlayback.connect", h.Connect, trpcgo.Procedure().Use(middleware.TRPCCredentialTransport("")))
	trpcgo.MustVoidQuery(router, "twitchPlayback.status", h.Status)
	trpcgo.MustVoidMutation(router, "twitchPlayback.check", h.Check)
	trpcgo.MustVoidMutation(router, "twitchPlayback.disconnect", h.Disconnect)
	server := httptest.NewServer(trpc.NewHandler(router, "/trpc"))
	defer server.Close()
	const token = "synthetic-session-0123456789abcdef"
	call := func(method, path, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+"/trpc/"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", server.URL)
		req.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(token)) {
			t.Fatal("credential returned by API")
		}
		return response.StatusCode, string(raw)
	}
	body, _ := json.Marshal(TwitchPlaybackConnectInput{SessionToken: token, Consent: true})
	// Even a valid credential must be rejected on a remote HTTP API before
	// validation or persistence, regardless of spoofed proxy headers.
	insecure := httptest.NewRequest(http.MethodPost, "http://remote.example/trpc/twitchPlayback.connect", bytes.NewReader(body))
	insecure.Header.Set("Origin", "http://remote.example")
	insecure.Header.Set("Content-Type", "application/json")
	insecure.Header.Set("X-Forwarded-Proto", "https")
	blocked := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(blocked, insecure)
	if blocked.Code != http.StatusBadRequest || v.calls.Load() != 0 || strings.Contains(blocked.Body.String(), token) {
		t.Fatalf("insecure import reached validator or exposed the credential: status=%d", blocked.Code)
	}

	if code, _ := call("POST", "twitchPlayback.connect", `{"session_token":"`+token+`","consent":false}`); code != 400 || v.calls.Load() != 0 {
		t.Fatal("missing consent reached validator")
	}
	if code, _ := call("GET", "twitchPlayback.connect", ""); code < 400 || v.calls.Load() != 0 {
		t.Fatal("GET credential import allowed")
	}
	if code, response := call("POST", "twitchPlayback.connect", string(body)); code != 200 || !strings.Contains(response, `"connected"`) {
		t.Fatalf("connect=%d %s", code, response)
	}
	row, err := repo.GetTwitchPlaybackSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.EncryptedToken, []byte(token)) {
		t.Fatal("plaintext persisted")
	}
	v.status.Store(503)
	if code, _ := call("POST", "twitchPlayback.check", ""); code != 503 {
		t.Fatalf("check outage status=%d", code)
	}
	if code, response := call("GET", "twitchPlayback.status", ""); code != 200 || !strings.Contains(response, `"connected"`) {
		t.Fatalf("outage invalidated connection: %s", response)
	}
	v.status.Store(401)
	if code, _ := call("POST", "twitchPlayback.connect", string(body)); code != 400 {
		t.Fatalf("invalid replacement=%d", code)
	}
	current, err := repo.GetTwitchPlaybackSession(ctx)
	if err != nil || !bytes.Equal(row.EncryptedToken, current.EncryptedToken) {
		t.Fatal("invalid replacement overwrote credential")
	}
	if code, response := call("POST", "twitchPlayback.check", ""); code != 200 || !strings.Contains(response, `"reconnect_required"`) {
		t.Fatalf("revocation=%d %s", code, response)
	}
	v.status.Store(0)
	if code, _ := call("POST", "twitchPlayback.connect", string(body)); code != 200 {
		t.Fatal("reconnect failed")
	}
	if code, response := call("POST", "twitchPlayback.disconnect", ""); code != 200 || !strings.Contains(response, `"disconnected"`) {
		t.Fatalf("disconnect=%d %s", code, response)
	}
	if credential, err := h.svc.Token(ctx); credential != "" || err != nil {
		t.Fatal("disconnect did not restore anonymous playback")
	}
}
