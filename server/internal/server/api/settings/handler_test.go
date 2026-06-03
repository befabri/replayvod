package settings

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/trpcgo"
)

func requireTRPCCode(t *testing.T, err error, want trpcgo.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want tRPC code %v", want)
	}
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("error = %T (%v), want *trpcgo.Error", err, err)
	}
	if te.Code != want {
		t.Fatalf("tRPC code = %v, want %v", te.Code, want)
	}
}

func newHandler(t *testing.T) *Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	return NewHandler(New(repo, log), log)
}

func TestGet_RequiresAuth(t *testing.T) {
	h := newHandler(t)
	_, err := h.Get(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeUnauthorized)
}

func TestUpdate_RequiresAuth(t *testing.T) {
	h := newHandler(t)
	_, err := h.Update(context.Background(), UpdateInput{
		Timezone:       "UTC",
		DatetimeFormat: "ISO",
		Language:       "en",
	})
	requireTRPCCode(t, err, trpcgo.CodeUnauthorized)
}

func TestGet_LazyCreatesDefaults(t *testing.T) {
	// Insert the user first for the settings FK.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	_, err := repo.UpsertUser(context.Background(), &repository.User{ID: "u1", Login: "alice", DisplayName: "Alice", Role: "viewer"})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	h2 := NewHandler(New(repo, log), log)

	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1", Role: "viewer"})
	got, err := h2.Get(ctx)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "u1")
	}
	if got.Timezone != defaultTimezone {
		t.Errorf("Timezone = %q, want %q", got.Timezone, defaultTimezone)
	}
	if got.DatetimeFormat != defaultDatetimeFormat {
		t.Errorf("DatetimeFormat = %q, want %q", got.DatetimeFormat, defaultDatetimeFormat)
	}
	if got.Language != defaultLanguage {
		t.Errorf("Language = %q, want %q", got.Language, defaultLanguage)
	}
}

func TestUpdate_PersistsSettings(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	_, err := repo.UpsertUser(context.Background(), &repository.User{ID: "u2", Login: "bob", DisplayName: "Bob", Role: "viewer"})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	h := NewHandler(New(repo, log), log)

	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u2", Role: "viewer"})
	got, err := h.Update(ctx, UpdateInput{
		Timezone:       "America/New_York",
		DatetimeFormat: "US",
		Language:       "fr",
	})
	if err != nil {
		t.Fatalf("Update error = %v", err)
	}
	if got.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want %q", got.Timezone, "America/New_York")
	}
	if got.DatetimeFormat != "US" {
		t.Errorf("DatetimeFormat = %q, want %q", got.DatetimeFormat, "US")
	}
	if got.Language != "fr" {
		t.Errorf("Language = %q, want %q", got.Language, "fr")
	}
	if got.UserID != "u2" {
		t.Errorf("UserID = %q, want %q", got.UserID, "u2")
	}
}

func TestUpdate_IsolatedPerUser(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	for _, u := range []repository.User{
		{ID: "ua", Login: "alice", DisplayName: "Alice", Role: "viewer"},
		{ID: "ub", Login: "bob", DisplayName: "Bob", Role: "viewer"},
	} {
		u := u
		if _, err := repo.UpsertUser(context.Background(), &u); err != nil {
			t.Fatalf("upsert user %q: %v", u.ID, err)
		}
	}
	h := NewHandler(New(repo, log), log)

	ctxA := middleware.WithUser(context.Background(), &repository.User{ID: "ua", Role: "viewer"})
	ctxB := middleware.WithUser(context.Background(), &repository.User{ID: "ub", Role: "viewer"})

	if _, err := h.Update(ctxA, UpdateInput{Timezone: "Europe/Paris", DatetimeFormat: "EU", Language: "fr"}); err != nil {
		t.Fatalf("Update A: %v", err)
	}
	if _, err := h.Update(ctxB, UpdateInput{Timezone: "UTC", DatetimeFormat: "ISO", Language: "en"}); err != nil {
		t.Fatalf("Update B: %v", err)
	}

	gotA, err := h.Get(ctxA)
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}
	if gotA.Timezone != "Europe/Paris" {
		t.Errorf("user A Timezone = %q, want Europe/Paris", gotA.Timezone)
	}

	gotB, err := h.Get(ctxB)
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if gotB.Timezone != "UTC" {
		t.Errorf("user B Timezone = %q, want UTC", gotB.Timezone)
	}
}

func TestToResponse_FieldMapping(t *testing.T) {
	s := &repository.Settings{
		UserID:         "u3",
		Timezone:       "Asia/Tokyo",
		DatetimeFormat: "ISO",
		Language:       "en",
	}
	r := toResponse(s)
	if r.UserID != s.UserID {
		t.Errorf("UserID: %q != %q", r.UserID, s.UserID)
	}
	if r.Timezone != s.Timezone {
		t.Errorf("Timezone: %q != %q", r.Timezone, s.Timezone)
	}
	if r.DatetimeFormat != s.DatetimeFormat {
		t.Errorf("DatetimeFormat: %q != %q", r.DatetimeFormat, s.DatetimeFormat)
	}
	if r.Language != s.Language {
		t.Errorf("Language: %q != %q", r.Language, s.Language)
	}
}
