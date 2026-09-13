package video

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

type statisticsRepo struct {
	repository.Repository
	userID string
	count  int64
	err    error
}

func (r *statisticsRepo) VideoStatsTotals(_ context.Context, userID string) (*repository.VideoStatsTotals, error) {
	r.userID = userID
	return &repository.VideoStatsTotals{ContinueWatching: r.count}, r.err
}

func (r *statisticsRepo) VideoStatsByStatus(context.Context) ([]repository.VideoStatsByStatus, error) {
	return nil, nil
}

func TestStatisticsContinueWatching(t *testing.T) {
	for _, count := range []int64{0, 75} {
		repo := &statisticsRepo{count: count}
		h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}
		ctx := middleware.WithUser(context.Background(), &repository.User{ID: "viewer"})
		out, err := h.Statistics(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if repo.userID != "viewer" {
			t.Fatalf("statistics requested for %q", repo.userID)
		}
		body, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		value, ok := fields["continue_watching"]
		if !ok {
			t.Fatal("continue_watching missing from response")
		}
		var got int64
		if err := json.Unmarshal(value, &got); err != nil {
			t.Fatal(err)
		}
		if got != count {
			t.Fatalf("continue_watching = %d, want %d", got, count)
		}
	}
}

func TestStatisticsDoesNotExposeCountsWithoutSessionOrOnFailure(t *testing.T) {
	repo := &statisticsRepo{count: 75}
	h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}
	_, err := h.Statistics(context.Background())
	assertTRPCCode(t, err, trpcgo.CodeUnauthorized)
	if repo.userID != "" {
		t.Fatal("queried statistics without a session")
	}
	repo.err = errors.New("database unavailable")
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "viewer"})
	out, err := h.Statistics(ctx)
	if err == nil {
		t.Fatal("expected error, got partial statistics")
	}
	if out.ContinueWatching != 0 {
		t.Fatal("returned partial count on failure")
	}
}
