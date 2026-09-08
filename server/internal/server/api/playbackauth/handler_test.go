package playbackauth

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	svc "github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type validator struct{ calls int }

func (v *validator) Validate(context.Context, string) (svc.Identity, error) {
	v.calls++
	return svc.Identity{UserID: "123", Login: "viewer"}, nil
}

func TestConnectRequiresConsentAndNeverReturnsCredential(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	v := &validator{}
	h := &Handler{svc: svc.New(sqliteadapter.New(testdb.NewSQLiteDB(t)), strings.Repeat("s", 32), v), log: slog.New(slog.NewTextHandler(&logs, nil))}
	const token = "private-session-0123456789abcdef"
	if _, err := h.Connect(ctx, TwitchPlaybackConnectInput{SessionToken: token}); err == nil || v.calls != 0 {
		t.Fatal("missing consent accepted")
	}
	got, err := h.Connect(ctx, TwitchPlaybackConnectInput{SessionToken: token, Consent: true})
	if err != nil || got.State != "connected" {
		t.Fatalf("connect = %+v %v", got, err)
	}
	for _, response := range []TwitchPlaybackStatusResponse{got, mustStatus(t, h, ctx)} {
		encoded, _ := json.Marshal(response)
		if bytes.Contains(encoded, []byte(token)) || bytes.Contains(encoded, []byte("token")) {
			t.Fatal("credential in response")
		}
	}
	if strings.Contains(logs.String(), token) {
		t.Fatal("credential in logs")
	}
}

func mustStatus(t *testing.T, h *Handler, ctx context.Context) TwitchPlaybackStatusResponse {
	t.Helper()
	got, err := h.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
