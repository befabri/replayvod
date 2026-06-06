package video

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/trpcgo"
)

func ptrNow() *time.Time { now := time.Now(); return &now }

// fakeDeleteRepo serves a single video to the Delete handler's GetByID lookup.
type fakeDeleteRepo struct {
	repository.Repository
	video  *repository.Video
	getErr error
}

func (r *fakeDeleteRepo) GetVideo(context.Context, int64) (*repository.Video, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.video, nil
}

// fakeDeletionRequester records the queue call so tests can assert it ran or
// was skipped by a pre-check.
type fakeDeletionRequester struct {
	calls int
	err   error
	video *repository.Video
}

func (d *fakeDeletionRequester) RequestManualDelete(_ context.Context, v *repository.Video) error {
	d.calls++
	d.video = v
	return d.err
}

func TestDelete_QueuesTerminalRecording(t *testing.T) {
	for _, status := range []string{repository.VideoStatusDone, repository.VideoStatusFailed} {
		t.Run(status, func(t *testing.T) {
			repo := &fakeDeleteRepo{video: &repository.Video{ID: 7, Status: status}}
			del := &fakeDeletionRequester{}
			h := &Handler{video: New(repo, testClientLogger()), deletion: del, log: testClientLogger()}
			ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

			out, err := h.Delete(ctx, DeleteInput{ID: 7})
			if err != nil {
				t.Fatalf("Delete returned error: %v", err)
			}
			if !out.OK {
				t.Fatal("Delete returned OK{false}")
			}
			if del.calls != 1 {
				t.Fatalf("RequestManualDelete called %d times, want 1", del.calls)
			}
			if del.video == nil || del.video.ID != 7 {
				t.Fatalf("queued video = %+v, want id 7", del.video)
			}
		})
	}
}

func TestDelete_MapsUnavailableDeletionWorker(t *testing.T) {
	repo := &fakeDeleteRepo{video: &repository.Video{ID: 7, Status: repository.VideoStatusDone}}
	del := &fakeDeletionRequester{err: retention.ErrManualDeletionUnavailable}
	h := &Handler{video: New(repo, testClientLogger()), deletion: del, log: testClientLogger()}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

	_, err := h.Delete(ctx, DeleteInput{ID: 7})
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("err = %T (%v), want *trpcgo.Error", err, err)
	}
	if te.Code != trpcgo.CodeServiceUnavailable {
		t.Fatalf("code = %v, want %v (msg %q)", te.Code, trpcgo.CodeServiceUnavailable, te.Message)
	}
	if del.calls != 1 {
		t.Fatalf("RequestManualDelete called %d times, want 1", del.calls)
	}
}

func TestDelete_RejectsAndSkipsPurge(t *testing.T) {
	cases := []struct {
		name  string
		video *repository.Video
		err   error
		want  trpcgo.ErrorCode
	}{
		{
			"running recording must be cancelled first",
			&repository.Video{ID: 1, Status: repository.VideoStatusRunning},
			nil, trpcgo.CodeConflict,
		},
		{
			"pending recording must be cancelled first",
			&repository.Video{ID: 1, Status: repository.VideoStatusPending},
			nil, trpcgo.CodeConflict,
		},
		{
			"already removed",
			&repository.Video{ID: 1, Status: repository.VideoStatusDone, DeletedAt: ptrNow()},
			nil, trpcgo.CodeConflict,
		},
		{
			"missing recording",
			nil, repository.ErrNotFound, trpcgo.CodeNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeDeleteRepo{video: tc.video, getErr: tc.err}
			del := &fakeDeletionRequester{}
			h := &Handler{video: New(repo, testClientLogger()), deletion: del, log: testClientLogger()}
			ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

			_, err := h.Delete(ctx, DeleteInput{ID: 1})
			var te *trpcgo.Error
			if !errors.As(err, &te) {
				t.Fatalf("err = %T (%v), want *trpcgo.Error", err, err)
			}
			if te.Code != tc.want {
				t.Fatalf("code = %v, want %v (msg %q)", te.Code, tc.want, te.Message)
			}
			if del.calls != 0 {
				t.Fatalf("RequestManualDelete ran %d times; a rejected delete must not queue", del.calls)
			}
		})
	}
}

// TestDelete_ConcurrentRemovalMapsToConflict: a row tombstoned between the
// pre-check and the queue write surfaces ErrNotFound, which must map to the
// same already-removed conflict, not a bare not-found.
func TestDelete_ConcurrentRemovalMapsToConflict(t *testing.T) {
	repo := &fakeDeleteRepo{video: &repository.Video{ID: 7, Status: repository.VideoStatusDone}}
	del := &fakeDeletionRequester{err: fmt.Errorf("queue manual delete: %w", repository.ErrNotFound)}
	h := &Handler{video: New(repo, testClientLogger()), deletion: del, log: testClientLogger()}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

	_, err := h.Delete(ctx, DeleteInput{ID: 7})
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("err = %T (%v), want *trpcgo.Error", err, err)
	}
	if te.Code != trpcgo.CodeConflict {
		t.Fatalf("code = %v, want %v (msg %q)", te.Code, trpcgo.CodeConflict, te.Message)
	}
	if del.calls != 1 {
		t.Fatalf("RequestManualDelete called %d times, want 1 (the queue write was attempted)", del.calls)
	}
}
