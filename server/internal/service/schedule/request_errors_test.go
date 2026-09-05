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

type rejectResultRepo struct {
	repository.Repository
	decided     bool
	decideErr   error
	lookupErr   error
	lookupCalls int
}

func (r *rejectResultRepo) DecideScheduleRequest(context.Context, int64, string, string, *int64) (bool, error) {
	return r.decided, r.decideErr
}
func (r *rejectResultRepo) GetScheduleRequest(context.Context, int64) (*repository.ScheduleRequest, error) {
	r.lookupCalls++
	return &repository.ScheduleRequest{Status: repository.ScheduleRequestStatusRejected}, r.lookupErr
}

func TestRejectRequest_ErrorClassification(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name                          string
		decided                       bool
		decideErr, lookupErr, wantErr error
		wantLookups                   int
	}{
		{"success", true, nil, dbErr, nil, 0},
		{"write failure", false, dbErr, nil, dbErr, 0},
		{"already decided", false, nil, nil, ErrRequestAlreadyDecided, 1},
		{"missing", false, nil, fmt.Errorf("lookup: %w", repository.ErrNotFound), ErrRequestNotFound, 1},
		{"read failure", false, nil, dbErr, dbErr, 1},
		{"cancelled", false, nil, context.Canceled, context.Canceled, 1},
		{"deadline", false, nil, context.DeadlineExceeded, context.DeadlineExceeded, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &rejectResultRepo{decided: tc.decided, decideErr: tc.decideErr, lookupErr: tc.lookupErr}
			svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err := svc.RejectRequest(context.Background(), "admin", 1); !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if repo.lookupCalls != tc.wantLookups {
				t.Errorf("lookups = %d, want %d", repo.lookupCalls, tc.wantLookups)
			}
		})
	}
}
