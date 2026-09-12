package video

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/replayvod/server/internal/service/storagescan"
	"github.com/befabri/replayvod/server/internal/storage"
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

func missingTombstone(id int64) *repository.Video {
	kind := repository.DeletionKindMissing
	return &repository.Video{ID: id, Status: repository.VideoStatusDone, DeletedAt: ptrNow(), DeletionKind: &kind}
}

// TestDelete_MissingTombstoneIsRemovedPermanently pins the one removed row
// that may be removed again: a missing-media tombstone still owns objects and
// part rows, so the manual delete queues; every other tombstone is a conflict.
func TestDelete_MissingTombstoneIsRemovedPermanently(t *testing.T) {
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})
	del := &fakeDeletionRequester{}
	h := &Handler{video: New(&fakeDeleteRepo{video: missingTombstone(7)}, testClientLogger()), deletion: del, log: testClientLogger()}
	if out, err := h.Delete(ctx, DeleteInput{ID: 7}); err != nil || !out.OK || del.calls != 1 {
		t.Fatalf("Delete(missing tombstone) = %+v, %v, calls %d; want queued", out, err, del.calls)
	}
	for _, kind := range []string{repository.DeletionKindManual, repository.DeletionKindRetention} {
		k := kind
		final := &fakeDeletionRequester{}
		h := &Handler{video: New(&fakeDeleteRepo{video: &repository.Video{ID: 8, Status: repository.VideoStatusDone, DeletedAt: ptrNow(), DeletionKind: &k}}, testClientLogger()), deletion: final, log: testClientLogger()}
		_, err := h.Delete(ctx, DeleteInput{ID: 8})
		var terr *trpcgo.Error
		if !errors.As(err, &terr) || terr.Code != trpcgo.CodeConflict || final.calls != 0 {
			t.Fatalf("Delete(%s tombstone) err = %v, calls %d; want conflict without queueing", kind, err, final.calls)
		}
	}
}

type fakeRestorer struct {
	err error
	ids []int64
}

func (r *fakeRestorer) Restore(_ context.Context, id int64) error {
	r.ids = append(r.ids, id)
	return r.err
}

func TestRestore_MapsScanOutcomes(t *testing.T) {
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})
	cases := []struct {
		name    string
		err     error
		code    trpcgo.ErrorCode
		message string
	}{
		{name: "restored", err: nil},
		{name: "recording not found", err: fmt.Errorf("get video: %w", repository.ErrNotFound), code: trpcgo.CodeNotFound, message: "not found"},
		{name: "still missing", err: &storagescan.StillMissingError{Missing: 2, Total: 5}, code: trpcgo.CodeConflict, message: "2 of 5 parts are still missing"},
		{name: "not a tombstone", err: storagescan.ErrNotRestorable, code: trpcgo.CodeConflict, message: "not a missing-media tombstone"},
		{name: "archived again", err: storagescan.ErrArchivedAgain, code: trpcgo.CodeConflict, message: "archived again"},
		{name: "unattached", err: fmt.Errorf("%w: marker missing", storage.ErrUnattached), code: trpcgo.CodeServiceUnavailable, message: "storage is not attached"},
		{name: "unreachable", err: fmt.Errorf("%w: stat", storage.ErrUnreachable), code: trpcgo.CodeServiceUnavailable, message: "storage is unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRestorer{err: tc.err}
			h := &Handler{restorer: r, log: testClientLogger()}
			out, err := h.Restore(ctx, RestoreInput{ID: 7})
			if len(r.ids) != 1 || r.ids[0] != 7 {
				t.Fatalf("restorer called with %v, want [7]", r.ids)
			}
			if tc.err == nil {
				if err != nil || !out.OK {
					t.Fatalf("Restore = %+v, %v; want OK", out, err)
				}
				return
			}
			var terr *trpcgo.Error
			if !errors.As(err, &terr) || terr.Code != tc.code || !strings.Contains(terr.Message, tc.message) {
				t.Fatalf("Restore err = %v, want code %d containing %q", err, tc.code, tc.message)
			}
		})
	}
}

func TestRestore_RequiresUserAndRestorer(t *testing.T) {
	h := &Handler{restorer: &fakeRestorer{}, log: testClientLogger()}
	if _, err := h.Restore(context.Background(), RestoreInput{ID: 7}); err == nil {
		t.Fatal("restore without a user succeeded")
	}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})
	var terr *trpcgo.Error
	if _, err := (&Handler{log: testClientLogger()}).Restore(ctx, RestoreInput{ID: 7}); !errors.As(err, &terr) || terr.Code != trpcgo.CodeServiceUnavailable {
		t.Fatalf("restore without a scan service err = %v, want SERVICE_UNAVAILABLE", err)
	}
}

type fakeHistoryStatsRepo struct {
	repository.Repository
	buckets []repository.VideoStatsHistoryBucket
}

func (r *fakeHistoryStatsRepo) VideoStatsHistory(context.Context) ([]repository.VideoStatsHistoryBucket, error) {
	return r.buckets, nil
}

// TestHistoryCounts_UnavailableIsTheRestorableSliceOfRemoved pins the third
// bucket the Unavailable filter needs: missing tombstones count as removed and
// as unavailable, other kinds only as removed.
func TestHistoryCounts_UnavailableIsTheRestorableSliceOfRemoved(t *testing.T) {
	repo := &fakeHistoryStatsRepo{buckets: []repository.VideoStatsHistoryBucket{
		{Status: repository.VideoStatusDone, CompletionKind: repository.CompletionKindComplete, Removed: false, Count: 5},
		{Status: repository.VideoStatusDone, CompletionKind: repository.CompletionKindComplete, Removed: true, DeletionKind: repository.DeletionKindMissing, Count: 2},
		{Status: repository.VideoStatusDone, CompletionKind: repository.CompletionKindComplete, Removed: true, DeletionKind: repository.DeletionKindManual, Count: 3},
		{Status: repository.VideoStatusFailed, CompletionKind: repository.CompletionKindPartial, Removed: true, DeletionKind: repository.DeletionKindMissing, Count: 1},
	}}
	counts, err := New(repo, testClientLogger()).HistoryCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts.All != (HistoryCount{OnDisk: 5, Removed: 6, Unavailable: 3}) {
		t.Fatalf("all = %+v", counts.All)
	}
	if counts.Completed != (HistoryCount{OnDisk: 5, Removed: 5, Unavailable: 2}) {
		t.Fatalf("completed = %+v", counts.Completed)
	}
	if counts.Failed != (HistoryCount{Removed: 1, Unavailable: 1}) {
		t.Fatalf("failed = %+v", counts.Failed)
	}
}
