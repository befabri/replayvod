package tag

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
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

func TestList_Empty(t *testing.T) {
	h := newHandler(t)
	got, err := h.List(context.Background())
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if got == nil {
		t.Fatal("result is nil, want non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestList_ReturnsSeededTags(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	now := time.Now().UTC().Truncate(time.Second)
	if err := seedTag(repo, "gaming"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := NewHandler(New(repo, log), log)
	got, err := h.List(context.Background())
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "gaming" {
		t.Errorf("Name = %q, want gaming", got[0].Name)
	}
	if got[0].ID <= 0 {
		t.Errorf("ID = %d, want > 0", got[0].ID)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if diff := got[0].CreatedAt.Sub(now); diff < -5e9 || diff > 5e9 {
		t.Errorf("CreatedAt = %v, want near %v", got[0].CreatedAt, now)
	}
}

func TestList_ErrMapsToInternal(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		svc: &Service{repo: &errorRepo{err: errors.New("db down")}, log: log},
		log: log,
	}
	_, err := h.List(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeInternalServerError)
}

func TestList_ErrNotFoundMapsToNotFound(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		svc: &Service{repo: &errorRepo{err: repository.ErrNotFound}, log: log},
		log: log,
	}
	_, err := h.List(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
}

func seedTag(repo repository.Repository, name string) error {
	_, err := repo.UpsertTag(context.Background(), name)
	return err
}

type errorRepo struct {
	err                   error
	repository.Repository // embed interface for unused methods — will panic if called
}

func (r *errorRepo) ListTags(_ context.Context) ([]repository.Tag, error) {
	return nil, r.err
}
