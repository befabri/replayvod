package schedule

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

type scheduleReadCounter struct {
	repository.Repository
	calls   int
	userIDs []string
	nameErr error
}

func (r *scheduleReadCounter) GetUser(ctx context.Context, id string) (*repository.User, error) {
	r.calls++
	return r.Repository.GetUser(ctx, id)
}
func (r *scheduleReadCounter) ListScheduleCategories(ctx context.Context, id int64) ([]repository.Category, error) {
	r.calls++
	return r.Repository.ListScheduleCategories(ctx, id)
}
func (r *scheduleReadCounter) ListScheduleTags(ctx context.Context, id int64) ([]repository.Tag, error) {
	r.calls++
	return r.Repository.ListScheduleTags(ctx, id)
}
func (r *scheduleReadCounter) ListUserDisplayNames(ctx context.Context, ids []string) (map[string]string, error) {
	r.calls++
	r.userIDs = ids
	if r.nameErr != nil {
		return nil, r.nameErr
	}
	return r.Repository.ListUserDisplayNames(ctx, ids)
}
func (r *scheduleReadCounter) ListScheduleCategoriesByScheduleIDs(ctx context.Context, ids []int64) (map[int64][]repository.Category, error) {
	r.calls++
	return r.Repository.ListScheduleCategoriesByScheduleIDs(ctx, ids)
}
func (r *scheduleReadCounter) ListScheduleTagsByScheduleIDs(ctx context.Context, ids []int64) (map[int64][]repository.Tag, error) {
	r.calls++
	return r.Repository.ListScheduleTagsByScheduleIDs(ctx, ids)
}

func TestList_BatchesScheduleMetadata(t *testing.T) {
	ctx := context.Background()
	_, repo := newRequestTestService(t)
	requester := "viewer-1"
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("batch-%02d", i)
		if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: id}); err != nil {
			t.Fatal(err)
		}
		var from *string
		if i%2 == 0 {
			from = &requester
		}
		if _, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: id, RequestedBy: "admin-1", RequestedFrom: from, Quality: "HIGH"}); err != nil {
			t.Fatal(err)
		}
	}
	counter := &scheduleReadCounter{Repository: repo}
	svc := New(counter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	views, err := svc.List(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 50 {
		t.Fatalf("views=%d", len(views))
	}
	user, err := repo.GetUser(ctx, requester)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		want := ""
		if v.Schedule.RequestedFrom != nil {
			want = user.DisplayName
		}
		if v.RequestedFromName != want {
			t.Errorf("name=%q, want %q", v.RequestedFromName, want)
		}
	}
	if counter.calls > 3 {
		t.Errorf("metadata queries=%d, want at most 3 regardless of page size", counter.calls)
	}
	if len(counter.userIDs) != 1 || counter.userIDs[0] != requester {
		t.Errorf("requester IDs not deduplicated: %v", counter.userIDs)
	}
	counter.calls = 0
	if views, err := svc.List(ctx, 50, 50); err != nil || len(views) != 0 {
		t.Fatalf("empty page: %v %v", views, err)
	}
	if counter.calls != 0 {
		t.Errorf("empty page made %d metadata queries", counter.calls)
	}
	counter.nameErr = errors.New("requester lookup failed")
	if _, err := svc.List(ctx, 50, 0); !errors.Is(err, counter.nameErr) {
		t.Fatalf("requester failure was hidden: %v", err)
	}
}
