package contracttest

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testScheduleRequestPagination(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "alice", "channel")
	SeedUserChannel(t, ctx, repo, "bob", "channel")
	for i := 0; i < 6; i++ {
		user := "alice"
		if i%2 == 1 {
			user = "bob"
		}
		r, err := repo.CreateScheduleRequest(ctx, "channel", user, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.DecideScheduleRequest(ctx, r.ID, repository.ScheduleRequestStatusRejected, "bob", nil); err != nil {
			t.Fatal(err)
		}
	}
	h.BackdateScheduleRequests(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, user := range []string{"", "alice", "bob", "unknown"} {
		t.Run(user, func(t *testing.T) {
			list := func(limit int, cursor *repository.ScheduleRequestCursor) ([]repository.ScheduleRequestView, error) {
				if user == "" {
					return repo.ListScheduleRequests(ctx, limit, cursor)
				}
				return repo.ListScheduleRequestsForUser(ctx, user, limit, cursor)
			}
			all, err := list(50, nil)
			if err != nil {
				t.Fatal(err)
			}
			var cursor *repository.ScheduleRequestCursor
			got := make([]repository.ScheduleRequestView, 0)
			for range 10 {
				page, err := list(2, cursor)
				if err != nil {
					t.Fatal(err)
				}
				if len(page) > 2 {
					t.Fatalf("unbounded page: %d", len(page))
				}
				if len(page) == 0 {
					break
				}
				got = append(got, page...)
				last := page[len(page)-1]
				cursor = &repository.ScheduleRequestCursor{CreatedAt: last.CreatedAt, ID: last.ID}
			}
			if !reflect.DeepEqual(got, all) {
				t.Fatalf("pagination skipped/duplicated scoped history: got=%+v want=%+v", got, all)
			}
		})
	}
	first, err := repo.ListScheduleRequests(ctx, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	last := first[1]
	// New rows cannot shift the remaining page.
	if _, err := repo.CreateScheduleRequest(ctx, "channel", "alice", nil); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListScheduleRequests(ctx, 2, &repository.ScheduleRequestCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	if err != nil || len(page) != 2 || page[0].ID >= last.ID {
		t.Fatalf("insert shifted continuation: %+v %v", page, err)
	}
	for i := range 3 {
		channel := fmt.Sprintf("pending-%d", i)
		SeedUserChannel(t, ctx, repo, "alice", channel)
		if _, err := repo.CreateScheduleRequest(ctx, channel, "alice", nil); err != nil {
			t.Fatal(err)
		}
	}
	all, err := repo.ListScheduleRequests(ctx, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	anchor := all[1]
	if ok, err := repo.DeleteScheduleRequest(ctx, anchor.ID, anchor.RequestedBy); err != nil || !ok {
		t.Fatalf("cancel cursor row: %v %v", ok, err)
	}
	page, err = repo.ListScheduleRequests(ctx, 2, &repository.ScheduleRequestCursor{CreatedAt: anchor.CreatedAt, ID: anchor.ID})
	if err != nil || !reflect.DeepEqual(page, all[2:4]) {
		t.Fatalf("deleted cursor shifted continuation: %+v %v", page, err)
	}
}

func testScheduleBatchMetadata(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "user", "channel")
	ids := make([]int64, 0, 2)
	for i := 0; i < 2; i++ {
		user := fmt.Sprintf("author-%d", i)
		SeedUserChannel(t, ctx, repo, user, "channel")
		s, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "channel", RequestedBy: user, Quality: "HIGH"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	for _, id := range []string{"b", "a"} {
		if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
		if err := repo.LinkScheduleCategory(ctx, ids[0], id); err != nil {
			t.Fatal(err)
		}
		tag, err := repo.UpsertTag(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.LinkScheduleTag(ctx, ids[1], tag.ID); err != nil {
			t.Fatal(err)
		}
	}
	cats, err := repo.ListScheduleCategoriesByScheduleIDs(ctx, append(ids, 99999, ids[0]))
	if err != nil || len(cats) != 1 || len(cats[ids[0]]) != 2 || cats[ids[0]][0].Name != "a" {
		t.Fatalf("category batch scope/order: %+v %v", cats, err)
	}
	tags, err := repo.ListScheduleTagsByScheduleIDs(ctx, ids)
	if err != nil || len(tags) != 1 || len(tags[ids[1]]) != 2 || tags[ids[1]][0].Name != "a" {
		t.Fatalf("tag batch scope/order: %+v %v", tags, err)
	}
	names, err := repo.ListUserDisplayNames(ctx, []string{"user", "user", "missing"})
	user, userErr := repo.GetUser(ctx, "user")
	if err != nil || userErr != nil || len(names) != 1 || names["user"] != user.DisplayName {
		t.Fatalf("requester batch: %+v %v %v", names, err, userErr)
	}
	if rows, err := repo.ListScheduleCategoriesByScheduleIDs(ctx, nil); err != nil || len(rows) != 0 {
		t.Fatalf("empty category batch: %+v %v", rows, err)
	}
	if rows, err := repo.ListScheduleTagsByScheduleIDs(ctx, nil); err != nil || len(rows) != 0 {
		t.Fatalf("empty tag batch: %+v %v", rows, err)
	}
	if rows, err := repo.ListUserDisplayNames(ctx, nil); err != nil || len(rows) != 0 {
		t.Fatalf("empty name batch: %+v %v", rows, err)
	}
}
