package followsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(value any) *http.Response {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}
}

func newTestService(t *testing.T, repo repository.Repository, transport roundTripFunc, log *slog.Logger) *Service {
	t.Helper()
	client := twitch.NewClient("client", "secret", log)
	client.SetHTTPClient(&http.Client{Transport: transport})
	svc := New(repo, client, log)
	t.Cleanup(func() {
		svc.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := svc.Wait(ctx); err != nil {
			t.Errorf("follow sync shutdown: %v", err)
		}
	})
	return svc
}

func waitIdle(t *testing.T, svc *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := svc.work.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedUser(t *testing.T, repo repository.Repository) {
	t.Helper()
	if _, err := repo.UpsertUser(t.Context(), &repository.User{ID: "viewer", Login: "viewer", DisplayName: "Viewer", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
}

func TestLoginImportPaginatesEnrichesAndPersistsFollows(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	seedUser(t, repo)
	var pages, batches int
	svc := newTestService(t, repo, func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer user-token" {
			t.Error("import did not use the authenticated user's token")
		}
		switch req.URL.Path {
		case "/helix/channels/followed":
			pages++
			if req.URL.Query().Get("user_id") != "viewer" {
				t.Error("import requested another user's follows")
			}
			start, end, cursor := 1, 100, "next"
			if req.URL.Query().Get("after") == "next" {
				start, end, cursor = 101, 101, ""
			}
			var follows []map[string]any
			for i := start; i <= end; i++ {
				id := fmt.Sprint(i)
				follows = append(follows, map[string]any{"broadcaster_id": id, "broadcaster_login": "channel" + id, "broadcaster_name": "Channel " + id, "followed_at": "2026-01-01T00:00:00Z"})
			}
			return jsonResponse(map[string]any{"data": follows, "pagination": map[string]string{"cursor": cursor}}), nil
		case "/helix/users":
			batches++
			ids := req.URL.Query()["id"]
			if len(ids) == 0 || len(ids) > 100 {
				t.Errorf("invalid user enrichment batch size: %d", len(ids))
			}
			var users []map[string]string
			for _, id := range ids {
				users = append(users, map[string]string{"id": id, "profile_image_url": "https://example.com/" + id, "description": "About " + id})
			}
			return jsonResponse(map[string]any{"data": users}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", req.URL.Path)
		}
	}, slog.New(slog.DiscardHandler))
	if err := svc.Request("viewer", "user-token"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, svc)
	follows, err := repo.ListUserFollows(t.Context(), "viewer")
	if err != nil || len(follows) != 101 || pages != 2 || batches != 2 {
		t.Fatalf("import: follows=%d pages=%d batches=%d error=%v", len(follows), pages, batches, err)
	}
	for _, follow := range follows {
		if follow.ProfileImageURL == nil || *follow.ProfileImageURL != "https://example.com/"+follow.BroadcasterID || follow.Description == nil || *follow.Description != "About "+follow.BroadcasterID {
			t.Fatalf("channel enrichment missing: %+v", follow)
		}
	}
}

type failingFollowRepo struct {
	repository.Repository
	cancel context.CancelFunc
	err    error
}

func (r *failingFollowRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		fault := *r
		fault.Repository = tx
		return fn(&fault)
	})
}

func (r *failingFollowRepo) UpsertUserFollow(ctx context.Context, follow *repository.UserFollow) error {
	if err := r.Repository.UpsertUserFollow(ctx, follow); err != nil {
		return err
	}
	if follow.BroadcasterID == "2" {
		if r.cancel != nil {
			r.cancel()
		}
		return r.err
	}
	return nil
}

func TestFailedOrCancelledImportDoesNotCommitPartialFollows(t *testing.T) {
	for _, failure := range []string{"write", "cancellation", "enrichment"} {
		t.Run(failure, func(t *testing.T) {
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			seedUser(t, repo)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fault := &failingFollowRepo{Repository: repo, err: errors.New("follow write failed")}
			if failure == "cancellation" {
				fault.cancel, fault.err = cancel, context.Canceled
			}
			svc := newTestService(t, fault, func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/users") {
					if failure == "enrichment" {
						return nil, errors.New("enrichment unavailable")
					}
					return jsonResponse(map[string]any{"data": []any{}}), nil
				}
				return jsonResponse(map[string]any{"data": []map[string]string{
					{"broadcaster_id": "1", "broadcaster_login": "first", "broadcaster_name": "First", "followed_at": "2026-01-01T00:00:00Z"},
					{"broadcaster_id": "2", "broadcaster_login": "second", "broadcaster_name": "Second", "followed_at": "2026-01-01T00:00:00Z"},
				}}), nil
			}, slog.New(slog.DiscardHandler))
			if err := svc.sync(ctx, "viewer", "user-token"); err == nil {
				t.Fatal("failed import reported success")
			} else if failure != "enrichment" && !errors.Is(err, fault.err) {
				t.Fatalf("import returned an unrelated failure: %v", err)
			}
			if follows, err := repo.ListUserFollows(t.Context(), "viewer"); err != nil || len(follows) != 0 {
				t.Fatalf("partial follows committed: %+v, %v", follows, err)
			}
			for _, id := range []string{"1", "2"} {
				if _, err := repo.GetChannel(t.Context(), id); !errors.Is(err, repository.ErrNotFound) {
					t.Fatalf("partial channel %s committed: %v", id, err)
				}
			}
		})
	}
}

func TestConcurrentLoginsDeduplicateBoundAndJoinImports(t *testing.T) {
	started, cancelled := make(chan string, 3), make(chan string, 3)
	release := make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	svc := newTestService(t, nil, func(req *http.Request) (*http.Response, error) {
		id := req.URL.Query().Get("user_id")
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > syncTimeout {
			t.Error("import has no bounded execution deadline")
		}
		started <- id
		<-req.Context().Done()
		cancelled <- id
		<-release
		return nil, req.Context().Err()
	}, slog.New(slog.DiscardHandler))
	for _, id := range []string{"first", "second"} {
		if err := svc.Request(id, "user-token"); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-started:
			if got != id {
				t.Fatalf("started %s, want %s", got, id)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("import did not start")
		}
	}
	for _, id := range []string{"first", "third", "third"} {
		if err := svc.Request(id, "user-token"); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case id := <-started:
		t.Fatalf("duplicate or excess import started: %s", id)
	case <-time.After(20 * time.Millisecond):
	}
	svc.Stop()
	for range 2 {
		select {
		case <-cancelled:
		case <-time.After(5 * time.Second):
			t.Fatal("shutdown did not cancel an import")
		}
	}
	if err := svc.Request("fourth", "user-token"); !errors.Is(err, background.ErrStopped) {
		t.Fatalf("admission after shutdown: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := svc.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown released ownership before HTTP calls returned: %v", err)
	}
	unblock.Do(func() { close(release) })
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := svc.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-started:
		t.Fatalf("shutdown launched a queued import: %s", id)
	default:
	}
}

func TestPanickedImportReleasesUserForNextLogin(t *testing.T) {
	var calls atomic.Int32
	var logs bytes.Buffer
	svc := newTestService(t, nil, func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			panic("follow provider failed")
		}
		return jsonResponse(map[string]any{"data": []any{}}), nil
	}, slog.New(slog.NewTextHandler(&logs, nil)))
	for range 2 {
		if err := svc.Request("viewer", "user-token"); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, svc)
	}
	if calls.Load() != 2 || !strings.Contains(logs.String(), "worker panic: follow provider failed") {
		t.Fatalf("panic was not contained and retryable: calls=%d logs=%s", calls.Load(), logs.String())
	}
}
