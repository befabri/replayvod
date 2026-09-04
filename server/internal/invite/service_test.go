package invite

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestCreateKeepsCredentialsOutOfLogsAndList(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "admin", Login: "admin", DisplayName: "Admin", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	svc := New(repo, "https://dashboard.example", slog.New(slog.NewJSONHandler(&logs, nil)))
	raw, created, err := svc.Create(ctx, "admin", "viewer", time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || created.TokenHash == raw || created.TokenHash != HashToken(raw) {
		t.Fatalf("raw token/hash split was lost: %+v", created)
	}
	if svc.URL(raw) != "https://dashboard.example/invite/"+raw {
		t.Fatalf("shareable URL = %q", svc.URL(raw))
	}
	rows, err := svc.List(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != created.ID || rows[0].TokenHash == raw {
		t.Fatalf("list = %+v, %v", rows, err)
	}
	if strings.Contains(logs.String(), raw) || strings.Contains(logs.String(), created.TokenHash) || !strings.Contains(logs.String(), "invite created") {
		t.Fatal("creation logs must identify the action without exposing the token or hash")
	}
	if err := svc.Revoke(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeat revoke = %v, want ErrNotFound", err)
	}
	if ok, err := repo.RedeemInvite(ctx, created.TokenHash, "viewer"); err != nil || ok {
		t.Fatalf("revoked invite redeemed = %v, %v", ok, err)
	}
}

func TestCreateWriteFailureReturnsNoShareableToken(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	var logs bytes.Buffer
	svc := New(repo, "https://dashboard.example", slog.New(slog.NewJSONHandler(&logs, nil)))
	// The issuer foreign key deliberately fails; no unusable URL should escape.
	raw, inv, err := svc.Create(context.Background(), "missing-admin", "viewer", time.Hour, nil)
	if err == nil || raw != "" || inv != nil {
		t.Fatalf("failed creation = (%q, %+v, %v), want no token or row", raw, inv, err)
	}
	if strings.Contains(logs.String(), "invite created") {
		t.Fatal("failed creation was logged as successful")
	}
	rows, err := svc.List(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("failed creation persisted invite: %+v, %v", rows, err)
	}
}
