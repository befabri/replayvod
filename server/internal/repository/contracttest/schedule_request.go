package contracttest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testScheduleRequestDecideOnce(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-1", "b-1")

	note := "please record"
	req, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", &note)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != repository.ScheduleRequestStatusPending {
		t.Fatalf("created status = %q, want PENDING", req.Status)
	}

	sched, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH",
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	ok, err := repo.DecideScheduleRequest(ctx, req.ID, repository.ScheduleRequestStatusApproved, "u-1", &sched.ID)
	if err != nil || !ok {
		t.Fatalf("decide = %v, %v; want true", ok, err)
	}

	got, err := repo.GetScheduleRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after decide: %v", err)
	}
	if got.Status != repository.ScheduleRequestStatusApproved || got.DecidedAt == nil ||
		got.DecidedBy == nil || *got.DecidedBy != "u-1" ||
		got.ScheduleID == nil || *got.ScheduleID != sched.ID {
		t.Errorf("decided row not marked: %+v", got)
	}

	ok, err = repo.DecideScheduleRequest(ctx, req.ID, repository.ScheduleRequestStatusRejected, "u-1", nil)
	if err != nil {
		t.Fatalf("second decide: %v", err)
	}
	if ok {
		t.Error("second decision = true, want false (decided once)")
	}

	views, err := repo.ListScheduleRequestsForUser(ctx, "u-1", 50, nil)
	if err != nil {
		t.Fatalf("list for user: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("list for user = %d rows, want 1", len(views))
	}
	v := views[0]
	if v.BroadcasterLogin == "" || v.BroadcasterName == "" || v.RequestedByLogin == "" {
		t.Errorf("view display fields not hydrated: %+v", v)
	}
	if v.Note == nil || *v.Note != note {
		t.Errorf("view note = %v, want %q", v.Note, note)
	}
}

func testScheduleRequestApprovalFailureRollsBack(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "requester", "channel")
	SeedUserChannel(t, ctx, repo, "admin", "channel")
	req, err := repo.CreateScheduleRequest(ctx, "channel", "requester", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game", Name: "Game"}); err != nil {
		t.Fatal(err)
	}
	tag, err := repo.UpsertTag(ctx, "tag")
	if err != nil {
		t.Fatal(err)
	}
	input := &repository.ScheduleInput{BroadcasterID: "channel", RequestedBy: "admin", Quality: "HIGH", RequestedFrom: &req.RequestedBy}
	filters := repository.ScheduleFilterInput{CategoryIDs: []string{"game"}, TagIDs: []int64{tag.ID}}
	for _, tc := range []struct {
		name      string
		requestID int64
		decider   string
		filters   repository.ScheduleFilterInput
		wantError bool
	}{
		{"category foreign key", req.ID, "admin", repository.ScheduleFilterInput{CategoryIDs: []string{"missing"}}, true},
		{"tag failure after category link", req.ID, "admin", repository.ScheduleFilterInput{CategoryIDs: []string{"game"}, TagIDs: []int64{-1}}, true},
		{"decision failure after both links", req.ID, "missing-admin", filters, true},
		{"missing request after both links", req.ID + 999, "admin", filters, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sched, ok, err := repo.ApproveScheduleRequest(ctx, tc.requestID, tc.decider, input, tc.filters)
			if (err != nil) != tc.wantError || ok || sched != nil {
				t.Fatalf("failed approval = (%+v, %v, %v), want no schedule, false, error=%v", sched, ok, err, tc.wantError)
			}
			stored, err := repo.GetScheduleRequest(ctx, req.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(stored, req) {
				t.Fatalf("failed approval changed request: got %+v, want %+v", stored, req)
			}
			rows, err := repo.ListSchedules(ctx, 50, 0)
			if err != nil || len(rows) != 0 {
				t.Fatalf("failed approval left schedules: %+v, %v", rows, err)
			}
		})
	}
	sched, ok, err := repo.ApproveScheduleRequest(ctx, req.ID, "admin", input, filters)
	if err != nil || !ok || sched == nil {
		t.Fatalf("retry = (%+v, %v, %v)", sched, ok, err)
	}
	cats, err := repo.ListScheduleCategories(ctx, sched.ID)
	if err != nil || len(cats) != 1 || cats[0].ID != "game" {
		t.Fatalf("categories after retry = %+v, %v", cats, err)
	}
	tags, err := repo.ListScheduleTags(ctx, sched.ID)
	if err != nil || len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("tags after retry = %+v, %v", tags, err)
	}
}

func testScheduleRequestConcurrentApprovals(t *testing.T, h Harness) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := h.Repo()
	const contenders = 4
	requests := make([]*repository.ScheduleRequest, contenders)
	for i := range requests {
		id := fmt.Sprintf("user-%d", i)
		SeedUserChannel(t, ctx, repo, id, "channel")
		var err error
		requests[i], err = repo.CreateScheduleRequest(ctx, "channel", id, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		requestID int64
		schedule  *repository.DownloadSchedule
		ok        bool
		err       error
	}
	start := make(chan struct{})
	results := make(chan result, contenders)
	for _, req := range requests {
		go func() {
			<-start
			// Different authors ensure the per-author unique index cannot
			// substitute for the channel-wide active-schedule guard.
			sched, ok, err := repo.ApproveScheduleRequest(ctx, req.ID, req.RequestedBy,
				&repository.ScheduleInput{BroadcasterID: "channel", RequestedBy: req.RequestedBy, Quality: "HIGH"}, repository.ScheduleFilterInput{})
			results <- result{req.ID, sched, ok, err}
		}()
	}
	close(start)
	winners := 0
	for range contenders {
		r := <-results
		stored, err := repo.GetScheduleRequest(ctx, r.requestID)
		if err != nil {
			t.Fatal(err)
		}
		if r.ok {
			winners++
			if r.err != nil || r.schedule == nil || stored.Status != repository.ScheduleRequestStatusApproved || stored.ScheduleID == nil || *stored.ScheduleID != r.schedule.ID {
				t.Errorf("winning approval = %+v, stored request = %+v", r, stored)
			}
		} else if !errors.Is(r.err, repository.ErrDuplicate) || r.schedule != nil || stored.Status != repository.ScheduleRequestStatusPending || stored.DecidedAt != nil || stored.ScheduleID != nil {
			t.Errorf("losing approval = %+v, stored request = %+v; want duplicate and unchanged pending request", r, stored)
		}
	}
	rows, err := repo.ListSchedules(ctx, 50, 0)
	if err != nil || winners != 1 || len(rows) != 1 {
		t.Fatalf("winners=%d, schedules=%+v, err=%v; want exactly one committed approval", winners, rows, err)
	}
}

func testScheduleRequestListScopeAndHistory(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "alice", "channel-a")
	SeedUserChannel(t, ctx, repo, "bob", "channel-b")
	image := "https://example.test/channel.png"
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "channel-a", BroadcasterLogin: "channel_a", BroadcasterName: "Channel A", ProfileImageURL: &image}); err != nil {
		t.Fatal(err)
	}
	empty, err := repo.ListScheduleRequests(ctx, 50, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty list = %+v, %v", empty, err)
	}
	note := "Alice's private note"
	first, err := repo.CreateScheduleRequest(ctx, "channel-a", "alice", &note)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.DecideScheduleRequest(ctx, first.ID, repository.ScheduleRequestStatusRejected, "bob", nil); err != nil || !ok {
		t.Fatalf("reject = %v, %v", ok, err)
	}
	second, err := repo.CreateScheduleRequest(ctx, "channel-b", "bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	third, err := repo.CreateScheduleRequest(ctx, "channel-a", "alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := repo.ListScheduleRequests(ctx, 50, nil)
	if err != nil || len(all) != 3 || all[0].ID != third.ID || all[1].ID != second.ID || all[2].ID != first.ID {
		t.Fatalf("all requests = %+v, %v; want newest first including decided rows", all, err)
	}
	for _, tc := range []struct {
		user string
		want []repository.ScheduleRequestView
	}{
		{"alice", []repository.ScheduleRequestView{all[0], all[2]}},
		{"bob", []repository.ScheduleRequestView{all[1]}},
		{"unknown", []repository.ScheduleRequestView{}},
	} {
		got, err := repo.ListScheduleRequestsForUser(ctx, tc.user, 50, nil)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("requests for %s = %+v, %v; want %+v", tc.user, got, err, tc.want)
		}
	}
	alice, err := repo.GetUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	v := all[2]
	if v.BroadcasterLogin != "channel_a" || v.BroadcasterName != "Channel A" || v.ProfileImageURL == nil || *v.ProfileImageURL != image || v.RequestedByName != alice.DisplayName || v.RequestedByLogin != alice.Login || v.Note == nil || *v.Note != note || v.DecidedBy == nil || *v.DecidedBy != "bob" || v.DecidedAt == nil || v.Status != repository.ScheduleRequestStatusRejected {
		t.Fatalf("request history lost display or decision fields: %+v", v)
	}
}

// testScheduleRequestDuplicatePendingIsErrDuplicate checks that uniqueness
// races return a portable error.
func testScheduleRequestDuplicatePendingIsErrDuplicate(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-1", "b-1")

	if _, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", nil); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("duplicate pending create err = %v, want ErrDuplicate", err)
	}

	// Only pending requests participate in the unique index.
	views, err := repo.ListScheduleRequestsForUser(ctx, "u-1", 50, nil)
	if err != nil || len(views) != 1 {
		t.Fatalf("list = %d rows, %v; want 1", len(views), err)
	}
	if ok, err := repo.DecideScheduleRequest(ctx, views[0].ID, repository.ScheduleRequestStatusRejected, "u-1", nil); err != nil || !ok {
		t.Fatalf("decide: %v, %v", ok, err)
	}
	if _, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", nil); err != nil {
		t.Fatalf("re-create after decision: %v", err)
	}
}

// testScheduleRequestApproveAtomic checks that losing a decision race leaves no
// schedule behind.
func testScheduleRequestApproveAtomic(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-1", "b-1")
	SeedUserChannel(t, ctx, repo, "u-2", "b-2")

	req, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	requester := "u-2"
	sched, ok, err := repo.ApproveScheduleRequest(ctx, req.ID, "u-1",
		&repository.ScheduleInput{BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH", RequestedFrom: &requester},
		repository.ScheduleFilterInput{})
	if err != nil || !ok || sched == nil {
		t.Fatalf("approve = (%+v, %v, %v), want schedule + ok", sched, ok, err)
	}
	if sched.RequestedFrom == nil || *sched.RequestedFrom != requester {
		t.Errorf("returned schedule requested_from = %v, want %q", sched.RequestedFrom, requester)
	}
	reloaded, err := repo.GetSchedule(ctx, sched.ID)
	if err != nil {
		t.Fatalf("reload schedule: %v", err)
	}
	if reloaded.RequestedFrom == nil || *reloaded.RequestedFrom != requester {
		t.Errorf("reloaded schedule requested_from = %v, want %q", reloaded.RequestedFrom, requester)
	}
	got, err := repo.GetScheduleRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("reload request: %v", err)
	}
	if got.Status != repository.ScheduleRequestStatusApproved ||
		got.ScheduleID == nil || *got.ScheduleID != sched.ID ||
		got.DecidedBy == nil || *got.DecidedBy != "u-1" {
		t.Errorf("approved row not linked: %+v", got)
	}

	// A separate channel avoids the active-schedule guard so this reaches
	// the decision check.
	other, err := repo.CreateScheduleRequest(ctx, "b-2", "u-2", nil)
	if err != nil {
		t.Fatalf("create second request: %v", err)
	}
	if ok, err := repo.DecideScheduleRequest(ctx, other.ID, repository.ScheduleRequestStatusRejected, "u-1", nil); err != nil || !ok {
		t.Fatalf("pre-decide second request: %v, %v", ok, err)
	}
	lost, ok, err := repo.ApproveScheduleRequest(ctx, other.ID, "u-1",
		&repository.ScheduleInput{BroadcasterID: "b-2", RequestedBy: "u-2", Quality: "HIGH"},
		repository.ScheduleFilterInput{})
	if err != nil {
		t.Fatalf("approve decided request: %v", err)
	}
	if ok || lost != nil {
		t.Errorf("approve decided request = (%+v, %v), want (nil, false)", lost, ok)
	}
	scheds, err := repo.ListSchedules(ctx, 50, 0)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(scheds) != 1 {
		t.Errorf("schedules after lost approval = %d, want 1 (rolled back)", len(scheds))
	}
}

// testScheduleRequestApproveBlocksActiveDuplicate checks that a competing
// schedule leaves the request pending.
func testScheduleRequestApproveBlocksActiveDuplicate(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-1", "b-1")
	SeedUserChannel(t, ctx, repo, "u-2", "b-1")

	if _, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-2", Quality: "HIGH",
	}); err != nil {
		t.Fatalf("seed active schedule: %v", err)
	}
	req, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	sched, ok, err := repo.ApproveScheduleRequest(ctx, req.ID, "u-1",
		&repository.ScheduleInput{BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH"},
		repository.ScheduleFilterInput{})
	if !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("approve with active duplicate err = %v, want ErrDuplicate", err)
	}
	if ok || sched != nil {
		t.Errorf("approve with active duplicate = (%+v, %v), want (nil, false)", sched, ok)
	}

	got, err := repo.GetScheduleRequest(ctx, req.ID)
	if err != nil || got.Status != repository.ScheduleRequestStatusPending {
		t.Errorf("request after blocked approve = %+v, %v; want still PENDING", got, err)
	}
	scheds, err := repo.ListSchedules(ctx, 50, 0)
	if err != nil || len(scheds) != 1 {
		t.Errorf("schedules after blocked approve = %d, %v; want 1 (rolled back)", len(scheds), err)
	}
}

func testScheduleRequestCancelOwnPendingOnly(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-1", "b-1")

	req, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if ok, err := repo.DeleteScheduleRequest(ctx, req.ID, "someone-else"); err != nil {
		t.Fatalf("delete as other: %v", err)
	} else if ok {
		t.Error("delete as other user = true, want false")
	}

	if ok, err := repo.DecideScheduleRequest(ctx, req.ID, repository.ScheduleRequestStatusRejected, "u-1", nil); err != nil || !ok {
		t.Fatalf("decide: %v, %v", ok, err)
	}
	if ok, err := repo.DeleteScheduleRequest(ctx, req.ID, "u-1"); err != nil {
		t.Fatalf("delete decided: %v", err)
	} else if ok {
		t.Error("delete decided row = true, want false (audit row stays)")
	}

	pending, err := repo.CreateScheduleRequest(ctx, "b-1", "u-1", nil)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if ok, err := repo.DeleteScheduleRequest(ctx, pending.ID, "u-1"); err != nil || !ok {
		t.Fatalf("delete own pending = %v, %v; want true", ok, err)
	}
}
