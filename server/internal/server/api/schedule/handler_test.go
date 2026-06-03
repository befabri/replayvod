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
	_, err := h.Update(ctx, UpdateInput{ID: 999999, Quality: "HIGH"})
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
}

// TestUpdate_RequiresAuth proves the RequireUser guard fires before any service
// call when no user is on the context.
func TestUpdate_RequiresAuth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	h := NewHandler(schedulesvc.New(repo, log), log)

	_, err := h.Update(context.Background(), UpdateInput{ID: 1, Quality: "HIGH"})
	requireTRPCCode(t, err, trpcgo.CodeUnauthorized)
}
