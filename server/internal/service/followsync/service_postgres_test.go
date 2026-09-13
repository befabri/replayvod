//go:build integration

package followsync

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.SetupPG(m)) }

type firstChannelWrite struct {
	id       string
	acquired chan struct{}
}

// concurrentFollowRepo holds the first channel lock until imports overlap, exposing inconsistent lock ordering.
type concurrentFollowRepo struct {
	repository.Repository
	writes  chan firstChannelWrite
	release chan struct{}
	first   bool
}

func (r *concurrentFollowRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		wrapped := *r
		wrapped.Repository, wrapped.first = tx, true
		return fn(&wrapped)
	})
}

func (r *concurrentFollowRepo) UpsertChannel(ctx context.Context, channel *repository.Channel) (*repository.Channel, error) {
	if !r.first {
		return r.Repository.UpsertChannel(ctx, channel)
	}
	r.first = false
	select {
	case <-r.release:
		// A transaction retried after the initial overlap can proceed normally.
		return r.Repository.UpsertChannel(ctx, channel)
	default:
	}
	write := firstChannelWrite{id: channel.BroadcasterID, acquired: make(chan struct{})}
	select {
	case r.writes <- write:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	row, err := r.Repository.UpsertChannel(ctx, channel)
	if err != nil {
		return nil, err
	}
	close(write.acquired)
	select {
	case <-r.release:
		return row, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestConcurrentFollowImportsPersistBothUsersPostgres(t *testing.T) {
	repo := pgadapter.New(testdb.NewPGPool(t))
	for _, id := range []string{"first", "second"} {
		if _, err := repo.UpsertUser(t.Context(), &repository.User{ID: id, Login: id, DisplayName: id, Role: "viewer"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b"} {
		if _, err := repo.UpsertChannel(t.Context(), &repository.Channel{BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: id}); err != nil {
			t.Fatal(err)
		}
	}
	interleaved := &concurrentFollowRepo{Repository: repo, writes: make(chan firstChannelWrite, 2), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(interleaved.release) })
	var logs bytes.Buffer
	svc := newTestService(t, interleaved, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/users") {
			return jsonResponse(map[string]any{"data": []any{}}), nil
		}
		ids := []string{"a", "b"}
		if req.URL.Query().Get("user_id") == "second" {
			ids = []string{"b", "a"}
		}
		var follows []map[string]string
		for _, id := range ids {
			follows = append(follows, map[string]string{"broadcaster_id": id, "broadcaster_login": id, "broadcaster_name": id, "followed_at": "2026-01-01T00:00:00Z"})
		}
		return jsonResponse(map[string]any{"data": follows}), nil
	}, slog.New(slog.NewTextHandler(&logs, nil)))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	awaitWrite := func() firstChannelWrite {
		t.Helper()
		select {
		case write := <-interleaved.writes:
			return write
		case <-ctx.Done():
			t.Fatal("import did not reach its first channel write")
			return firstChannelWrite{}
		}
	}
	awaitLock := func(write firstChannelWrite) {
		t.Helper()
		select {
		case <-write.acquired:
		case <-ctx.Done():
			t.Fatalf("import did not acquire channel %s", write.id)
		}
	}
	if err := svc.Request("first", "first-token"); err != nil {
		t.Fatal(err)
	}
	first := awaitWrite()
	awaitLock(first)
	if err := svc.Request("second", "second-token"); err != nil {
		t.Fatal(err)
	}
	second := awaitWrite()
	if first.id != second.id {
		awaitLock(second)
	}
	release.Do(func() { close(interleaved.release) })
	if err := svc.work.WaitIdle(ctx); err != nil {
		t.Fatalf("overlapping imports did not settle: %v", err)
	}
	for _, id := range []string{"first", "second"} {
		follows, err := repo.ListUserFollows(t.Context(), id)
		if err != nil || len(follows) != 2 {
			t.Errorf("user %s imported %d follows, want both shared channels: %v; logs: %s", id, len(follows), err, logs.String())
		}
	}
}
