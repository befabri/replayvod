package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
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

// TestScheduleErrRules pins the package's shared domain mapping: this is the
// authorization boundary (ErrNotOwner -> 403) shared by every mutating
// procedure. A regression here turns a forbidden cross-user action into a 500.
func TestScheduleErrRules(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []struct {
		name string
		err  error
		want trpcgo.ErrorCode
	}{
		{"not owner -> forbidden", schedulesvc.ErrNotOwner, trpcgo.CodeForbidden},
		{"invalid filter -> bad request", schedulesvc.ErrInvalidFilter, trpcgo.CodeBadRequest},
		{"already scheduled -> bad request", schedulesvc.ErrAlreadyScheduled, trpcgo.CodeBadRequest},
		{"not found -> not found", repository.ErrNotFound, trpcgo.CodeNotFound},
		{"other -> internal", errors.New("db down"), trpcgo.CodeInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireTRPCCode(t, apierr.Map(log, tc.err, "update schedule", scheduleErrRules...), tc.want)
		})
	}
}

// TestUpdate_MissingScheduleIsNotFound exercises the full handler path
// (RequireUser -> service -> apierr.Map(scheduleErrRules)) end to end, proving
// the handler actually wires the rules and surfaces 404 for a missing row.
func TestUpdate_MissingScheduleIsNotFound(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	h := NewHandler(schedulesvc.New(repo, log), log)

	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1", Role: "viewer"})
	_, err := h.Update(ctx, UpdateInput{ID: 999999, ScheduleSettingsInput: ScheduleSettingsInput{Quality: "HIGH"}})
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
}

// TestUpdate_RequiresAuth proves the RequireUser guard fires before any service
// call when no user is on the context.
func TestUpdate_RequiresAuth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	h := NewHandler(schedulesvc.New(repo, log), log)

	_, err := h.Update(context.Background(), UpdateInput{ID: 1, ScheduleSettingsInput: ScheduleSettingsInput{Quality: "HIGH"}})
	requireTRPCCode(t, err, trpcgo.CodeUnauthorized)
}

// TestPauseState_RequiresAuth and TestSetPaused_RequiresAuth prove both global
// pause procedures gate on RequireUser before touching the service.
func TestPauseState_RequiresAuth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	h := NewHandler(schedulesvc.New(repo, log), log)

	_, err := h.PauseState(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeUnauthorized)
}

func TestSetPaused_RequiresAuth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	h := NewHandler(schedulesvc.New(repo, log), log)

	_, err := h.SetPaused(context.Background(), SetPausedInput{Paused: true})
	requireTRPCCode(t, err, trpcgo.CodeUnauthorized)
}

// TestAdminManagesForeignSchedule checks that handler ownership checks honor
// admin access.
func TestAdminManagesForeignSchedule(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	ctx := context.Background()
	for _, id := range []string{"author-1", "admin-1"} {
		if _, err := repo.UpsertUser(ctx, &repository.User{ID: id, Login: id, DisplayName: id, Role: "viewer"}); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	svc := schedulesvc.New(repo, log)
	created, err := svc.Create(ctx, "author-1", schedulesvc.WriteInput{BroadcasterID: "b-1", Quality: "HIGH"})
	if err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	h := NewHandler(svc, log)
	adminCtx := middleware.WithUser(context.Background(), &repository.User{ID: "admin-1", Role: "admin"})

	if _, err := h.GetByID(adminCtx, GetByIDInput{ID: created.Schedule.ID}); err != nil {
		t.Fatalf("admin GetByID foreign schedule: %v", err)
	}
	if _, err := h.Update(adminCtx, UpdateInput{ID: created.Schedule.ID, ScheduleSettingsInput: ScheduleSettingsInput{Quality: "LOW"}}); err != nil {
		t.Fatalf("admin Update foreign schedule: %v", err)
	}
	if _, err := h.Toggle(adminCtx, ToggleInput{ID: created.Schedule.ID}); err != nil {
		t.Fatalf("admin Toggle foreign schedule: %v", err)
	}
	if _, err := h.Delete(adminCtx, DeleteInput{ID: created.Schedule.ID}); err != nil {
		t.Fatalf("admin Delete foreign schedule: %v", err)
	}
}

// TestPauseState_RoundTrip exercises the full handler path end to end: defaults
// to false, SetPaused flips and echoes it, PauseState reads it back, then resume
// clears it.
func TestPauseState_RoundTrip(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	h := NewHandler(schedulesvc.New(repo, log), log)
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1", Role: "owner"})

	if got, err := h.PauseState(ctx); err != nil || got.Paused {
		t.Fatalf("PauseState (fresh) = (%+v, %v), want paused=false", got, err)
	}
	if got, err := h.SetPaused(ctx, SetPausedInput{Paused: true}); err != nil || !got.Paused {
		t.Fatalf("SetPaused(true) = (%+v, %v), want paused=true", got, err)
	}
	if got, err := h.PauseState(ctx); err != nil || !got.Paused {
		t.Fatalf("PauseState after pause = (%+v, %v), want paused=true", got, err)
	}
	if got, err := h.SetPaused(ctx, SetPausedInput{Paused: false}); err != nil || got.Paused {
		t.Fatalf("SetPaused(false) = (%+v, %v), want paused=false", got, err)
	}
}
