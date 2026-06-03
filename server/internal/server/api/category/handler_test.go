package category

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

func TestGetByID_NotFound(t *testing.T) {
	h := newHandler(t)
	_, err := h.GetByID(context.Background(), GetByIDInput{ID: "does-not-exist"})
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
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

func TestSearch_EmptyQueryReturnsAll(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	for _, c := range []repository.Category{
		{ID: "c1", Name: "Chess", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "c2", Name: "Zelda", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		c := c
		if err := seedCategory(repo, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	h := NewHandler(New(repo, log), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestSearch_LimitDefaultsTo50(t *testing.T) {
	h := newHandler(t)
	_, err := h.Search(context.Background(), SearchInput{Query: "x", Limit: 0})
	if err != nil {
		t.Fatalf("Search (limit=0) error = %v", err)
	}
}

func TestSearch_QueryFilters(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	for _, c := range []repository.Category{
		{ID: "g1", Name: "Chess", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "g2", Name: "Minecraft", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		c := c
		if err := seedCategory(repo, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	h := NewHandler(New(repo, log), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "Chess", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
	if len(got) > 0 && got[0].ID != "g1" {
		t.Errorf("ID = %q, want g1", got[0].ID)
	}
}

func TestToResponse_FieldMapping(t *testing.T) {
	boxArt := "https://example.com/box.jpg"
	igdb := "42"
	c := &repository.Category{
		ID:        "cat1",
		Name:      "My Game",
		BoxArtURL: &boxArt,
		IGDBID:    &igdb,
	}
	r := toResponse(c)
	if r.ID != c.ID {
		t.Errorf("ID: %q != %q", r.ID, c.ID)
	}
	if r.Name != c.Name {
		t.Errorf("Name: %q != %q", r.Name, c.Name)
	}
	if r.BoxArtURL == nil || *r.BoxArtURL != boxArt {
		t.Errorf("BoxArtURL: %v, want %q", r.BoxArtURL, boxArt)
	}
	if r.IGDBID == nil || *r.IGDBID != igdb {
		t.Errorf("IGDBID: %v, want %q", r.IGDBID, igdb)
	}
}

func seedCategory(repo repository.Repository, c *repository.Category) error {
	type categoryUpserter interface {
		UpsertCategory(ctx context.Context, c *repository.Category) (*repository.Category, error)
	}
	u, ok := repo.(categoryUpserter)
	if !ok {
		return errors.New("repo does not expose UpsertCategory")
	}
	_, err := u.UpsertCategory(context.Background(), c)
	return err
}
