package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func newRequestTestService(t *testing.T) (*Service, repository.Repository) {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	seedScheduleUser(t, ctx, repo, "viewer-1")
	seedScheduleUser(t, ctx, repo, "admin-1")
	seedScheduleChannel(t, ctx, repo, "b-1")
	return svc, repo
}

func TestCreateRequest_Guards(t *testing.T) {
	ctx := context.Background()
	svc, _ := newRequestTestService(t)

	if _, err := svc.CreateRequest(ctx, "viewer-1", "nope", nil); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown channel err = %v, want ErrNotFound", err)
	}

	if _, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil); !errors.Is(err, ErrRequestAlreadyExists) {
		t.Fatalf("duplicate pending err = %v, want ErrRequestAlreadyExists", err)
	}

	if _, err := svc.Create(ctx, "admin-1", WriteInput{BroadcasterID: "b-1", Quality: "HIGH"}); err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if _, err := svc.CreateRequest(ctx, "admin-1", "b-1", nil); !errors.Is(err, ErrAlreadyScheduled) {
		t.Fatalf("already scheduled err = %v, want ErrAlreadyScheduled", err)
	}
}

func TestApproveRequest_DisabledSchedulesDoNotBlockRequestsOrTriggerLive(t *testing.T) {
	ctx := context.Background()
	_, repo := newRequestTestService(t)
	trigger := &fakeLiveTrigger{}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))
	if _, err := svc.Create(ctx, "viewer-1", WriteInput{BroadcasterID: "b-1", Quality: "HIGH", IsDisabled: true}); err != nil {
		t.Fatal(err)
	}
	req, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil)
	if err != nil {
		t.Fatalf("a disabled schedule must not block filing: %v", err)
	}
	view, err := svc.ApproveRequest(ctx, "admin-1", req.ID, WriteInput{Quality: "HIGH", IsDisabled: true})
	if err != nil {
		t.Fatalf("approve as disabled: %v", err)
	}
	if !view.Schedule.IsDisabled || len(trigger.calls) != 0 {
		t.Fatalf("disabled approval triggered recording: schedule=%+v, calls=%+v", view.Schedule, trigger.calls)
	}
	stored, err := repo.GetScheduleRequest(ctx, req.ID)
	if err != nil || stored.Status != repository.ScheduleRequestStatusApproved || stored.ScheduleID == nil || *stored.ScheduleID != view.Schedule.ID {
		t.Fatalf("disabled approval not committed: %+v, %v", stored, err)
	}
}

func TestApproveRequest_ConcurrentActiveScheduleMapsToAlreadyScheduled(t *testing.T) {
	ctx := context.Background()
	_, repo := newRequestTestService(t)
	trigger := &fakeLiveTrigger{}
	svc := New(approveStubRepo{Repository: repo, err: repository.ErrDuplicate}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))
	req, err := repo.CreateScheduleRequest(ctx, "b-1", "viewer-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.ApproveRequest(ctx, "admin-1", req.ID, WriteInput{Quality: "HIGH"})
	if !errors.Is(err, ErrAlreadyScheduled) || view != nil || len(trigger.calls) != 0 {
		t.Fatalf("racing approval = (%+v, %v), triggers=%+v; want ErrAlreadyScheduled without a recording", view, err, trigger.calls)
	}
}

func TestApproveRequest_CreatesScheduleOnRequestedChannel(t *testing.T) {
	ctx := context.Background()
	svc, repo := newRequestTestService(t)

	req, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil)
	if err != nil {
		t.Fatalf("file request: %v", err)
	}

	view, err := svc.ApproveRequest(ctx, "admin-1", req.ID, WriteInput{BroadcasterID: "evil", Quality: "HIGH"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if view.Schedule.BroadcasterID != "b-1" {
		t.Fatalf("schedule broadcaster = %q, want b-1", view.Schedule.BroadcasterID)
	}
	if view.Schedule.RequestedBy != "admin-1" {
		t.Fatalf("schedule requested_by = %q, want admin-1 (the approving admin)", view.Schedule.RequestedBy)
	}
	if view.Schedule.RequestedFrom == nil || *view.Schedule.RequestedFrom != "viewer-1" {
		t.Fatalf("schedule requested_from = %v, want viewer-1 (the requester)", view.Schedule.RequestedFrom)
	}

	got, err := repo.GetScheduleRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("reload request: %v", err)
	}
	if got.Status != repository.ScheduleRequestStatusApproved || got.ScheduleID == nil || *got.ScheduleID != view.Schedule.ID {
		t.Fatalf("request not consumed: %+v", got)
	}

	if _, err := svc.ApproveRequest(ctx, "admin-1", req.ID, WriteInput{Quality: "HIGH"}); !errors.Is(err, ErrRequestAlreadyDecided) {
		t.Fatalf("second approve err = %v, want ErrRequestAlreadyDecided", err)
	}
	if err := svc.RejectRequest(ctx, "admin-1", req.ID); !errors.Is(err, ErrRequestAlreadyDecided) {
		t.Fatalf("reject after approve err = %v, want ErrRequestAlreadyDecided", err)
	}
}

// approveStubRepo simulates an approval race or transaction error without
// database writes.
type approveStubRepo struct {
	repository.Repository
	ok  bool
	err error
}

func (r approveStubRepo) ApproveScheduleRequest(context.Context, int64, string, *repository.ScheduleInput, repository.ScheduleFilterInput) (*repository.DownloadSchedule, bool, error) {
	return nil, r.ok, r.err
}

// TestApproveRequest_LostRaceNeverTriggersLive checks that losing approval
// cannot start recording.
func TestApproveRequest_LostRaceNeverTriggersLive(t *testing.T) {
	ctx := context.Background()
	_, repo := newRequestTestService(t)
	trigger := &fakeLiveTrigger{}
	svc := New(approveStubRepo{Repository: repo, ok: false}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	req, err := repo.CreateScheduleRequest(ctx, "b-1", "viewer-1", nil)
	if err != nil {
		t.Fatalf("file request: %v", err)
	}
	if _, err := svc.ApproveRequest(ctx, "admin-1", req.ID, WriteInput{Quality: "HIGH"}); !errors.Is(err, ErrRequestAlreadyDecided) {
		t.Fatalf("approve err = %v, want ErrRequestAlreadyDecided", err)
	}
	if len(trigger.calls) != 0 {
		t.Fatalf("live trigger fired %d times on a lost approval, want 0", len(trigger.calls))
	}
}

// TestApproveRequest_TxErrorNeverTriggersLive checks that failed approval
// cannot start recording.
func TestApproveRequest_TxErrorNeverTriggersLive(t *testing.T) {
	ctx := context.Background()
	_, repo := newRequestTestService(t)
	trigger := &fakeLiveTrigger{}
	svc := New(approveStubRepo{Repository: repo, err: errors.New("db down")}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	req, err := repo.CreateScheduleRequest(ctx, "b-1", "viewer-1", nil)
	if err != nil {
		t.Fatalf("file request: %v", err)
	}
	if _, err := svc.ApproveRequest(ctx, "admin-1", req.ID, WriteInput{Quality: "HIGH"}); err == nil {
		t.Fatal("approve with failing transaction must error")
	}
	if len(trigger.calls) != 0 {
		t.Fatalf("live trigger fired %d times on a failed approval, want 0", len(trigger.calls))
	}
}

// TestApproveRequest_TriggersLiveAfterDecision checks that recording starts
// only after approval commits.
func TestApproveRequest_TriggersLiveAfterDecision(t *testing.T) {
	ctx := context.Background()
	_, repo := newRequestTestService(t)
	trigger := &fakeLiveTrigger{}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	req, err := repo.CreateScheduleRequest(ctx, "b-1", "viewer-1", nil)
	if err != nil {
		t.Fatalf("file request: %v", err)
	}
	trigger.onCall = func(callCtx context.Context, scheduleID int64, broadcasterID string) {
		got, err := repo.GetScheduleRequest(callCtx, req.ID)
		if err != nil || got.Status != repository.ScheduleRequestStatusApproved || got.ScheduleID == nil || *got.ScheduleID != scheduleID {
			t.Fatalf("live trigger ran before approval was committed: request=%+v, err=%v", got, err)
		}
		if broadcasterID != req.BroadcasterID {
			t.Fatalf("live trigger broadcaster = %q, want %q", broadcasterID, req.BroadcasterID)
		}
		if err := repo.RecordScheduleTrigger(callCtx, scheduleID); err != nil {
			t.Fatal(err)
		}
	}
	view, err := svc.ApproveRequest(ctx, "admin-1", req.ID, WriteInput{Quality: "HIGH"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if len(trigger.calls) != 1 || trigger.calls[0].scheduleID != view.Schedule.ID {
		t.Fatalf("trigger calls = %+v, want exactly one for schedule %d", trigger.calls, view.Schedule.ID)
	}
	if view.Schedule.TriggerCount != 1 || view.Schedule.LastTriggeredAt == nil {
		t.Fatalf("approval returned stale trigger data: %+v", view.Schedule)
	}
	got, err := repo.GetScheduleRequest(ctx, req.ID)
	if err != nil || got.Status != repository.ScheduleRequestStatusApproved {
		t.Fatalf("request after approve = %+v, %v; want APPROVED", got, err)
	}
}

// TestApproveRequest_RechecksActiveScheduleAtDecision guards against duplicate
// schedules from competing requests.
func TestApproveRequest_RechecksActiveScheduleAtDecision(t *testing.T) {
	ctx := context.Background()
	svc, repo := newRequestTestService(t)
	seedScheduleUser(t, ctx, repo, "viewer-2")

	first, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil)
	if err != nil {
		t.Fatalf("file first request: %v", err)
	}
	second, err := svc.CreateRequest(ctx, "viewer-2", "b-1", nil)
	if err != nil {
		t.Fatalf("file second request: %v", err)
	}

	if _, err := svc.ApproveRequest(ctx, "admin-1", first.ID, WriteInput{Quality: "HIGH"}); err != nil {
		t.Fatalf("approve first: %v", err)
	}
	if _, err := svc.ApproveRequest(ctx, "admin-1", second.ID, WriteInput{Quality: "HIGH"}); !errors.Is(err, ErrAlreadyScheduled) {
		t.Fatalf("approve second err = %v, want ErrAlreadyScheduled", err)
	}

	scheds, err := repo.ListSchedules(ctx, 50, 0)
	if err != nil || len(scheds) != 1 {
		t.Fatalf("schedules = %d, %v; want exactly 1", len(scheds), err)
	}
	got, err := repo.GetScheduleRequest(ctx, second.ID)
	if err != nil || got.Status != repository.ScheduleRequestStatusPending {
		t.Fatalf("losing request = %+v, %v; want still PENDING", got, err)
	}
}

func TestCreateRequest_DuplicateInsertMapsToAlreadyExists(t *testing.T) {
	ctx := context.Background()
	_, repo := newRequestTestService(t)
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil); !errors.Is(err, ErrRequestAlreadyExists) {
		t.Fatalf("racing duplicate err = %v, want ErrRequestAlreadyExists", err)
	}
}

func TestRejectAndCancelRequest(t *testing.T) {
	ctx := context.Background()
	svc, _ := newRequestTestService(t)

	req, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil)
	if err != nil {
		t.Fatalf("file request: %v", err)
	}
	if err := svc.RejectRequest(ctx, "admin-1", req.ID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	page, err := svc.ListRequestsForUser(ctx, "viewer-1", 50, nil)
	views := page.Items
	if err != nil || len(views) != 1 {
		t.Fatalf("list mine = %d, %v; want 1 row", len(views), err)
	}
	if views[0].Status != repository.ScheduleRequestStatusRejected {
		t.Fatalf("status = %q, want REJECTED", views[0].Status)
	}

	again, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil)
	if err != nil {
		t.Fatalf("re-file after reject: %v", err)
	}
	if err := svc.CancelRequest(ctx, "other", again.ID); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("cancel as other err = %v, want ErrRequestNotFound", err)
	}
	if err := svc.CancelRequest(ctx, "viewer-1", again.ID); err != nil {
		t.Fatalf("cancel own: %v", err)
	}
	if err := svc.CancelRequest(ctx, "viewer-1", again.ID); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("cancel again err = %v, want ErrRequestNotFound", err)
	}
}

func TestCreateRequest_ConcurrentSubmissionsHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	svc, repo := newRequestTestService(t)
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		go func() { <-start; _, err := svc.CreateRequest(ctx, "viewer-1", "b-1", nil); results <- err }()
	}
	close(start)
	wins := 0
	for range 8 {
		err := <-results
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrRequestAlreadyExists) {
			t.Errorf("unexpected losing submission: %v", err)
		}
	}
	rows, err := repo.ListScheduleRequestsForUser(ctx, "viewer-1", 50, nil)
	if err != nil || wins != 1 || len(rows) != 1 {
		t.Fatalf("wins=%d rows=%v err=%v", wins, rows, err)
	}
}
