package video

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

type fakeVideoStateRepo struct {
	repository.Repository
	video         *repository.Video
	getErr        error
	getCalls      int
	setCalls      int
	progressCalls int
	observedAtMs  int64
}

func (r *fakeVideoStateRepo) GetVideo(_ context.Context, id int64) (*repository.Video, error) {
	r.getCalls++
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.video == nil || r.video.ID != id {
		return nil, repository.ErrNotFound
	}
	return r.video, nil
}

func (r *fakeVideoStateRepo) SetVideoWatchLater(_ context.Context, userID string, videoID int64, watchLater bool) (*repository.VideoUserState, error) {
	r.setCalls++
	now := time.Now()
	return &repository.VideoUserState{
		UserID:     userID,
		VideoID:    videoID,
		WatchLater: watchLater,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (r *fakeVideoStateRepo) UpdateVideoWatchProgress(_ context.Context, userID string, videoID int64, positionSeconds float64, completed bool, observedAtMs int64) (*repository.VideoUserState, error) {
	if r.video == nil || r.video.ID != videoID || r.video.DeletedAt != nil || r.video.Status != repository.VideoStatusDone {
		return nil, repository.ErrNotFound
	}
	r.progressCalls++
	r.observedAtMs = observedAtMs
	now := time.Now()
	state := &repository.VideoUserState{
		UserID:              userID,
		VideoID:             videoID,
		LastPositionSeconds: positionSeconds,
		LastProgressAtMs:    &observedAtMs,
		WatchedAt:           &now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if completed {
		state.CompletedAt = &now
	}
	return state, nil
}

func TestVideoUserStateMutationsValidateVideo(t *testing.T) {
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

	t.Run("missing video is not found", func(t *testing.T) {
		repo := &fakeVideoStateRepo{getErr: repository.ErrNotFound}
		h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}

		_, err := h.SetWatchLater(ctx, SetWatchLaterInput{VideoID: 7, WatchLater: true})
		assertTRPCCode(t, err, trpcgo.CodeNotFound)
		if repo.setCalls != 0 {
			t.Fatalf("SetVideoWatchLater called %d times, want 0", repo.setCalls)
		}
	})

	t.Run("running video can be saved for later", func(t *testing.T) {
		repo := &fakeVideoStateRepo{
			video: &repository.Video{ID: 7, Status: repository.VideoStatusRunning},
		}
		h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}

		out, err := h.SetWatchLater(ctx, SetWatchLaterInput{VideoID: 7, WatchLater: true})
		if err != nil {
			t.Fatalf("SetWatchLater returned error: %v", err)
		}
		if !out.WatchLater || repo.setCalls != 1 {
			t.Fatalf("watch later state = %+v, calls = %d", out, repo.setCalls)
		}
	})

	t.Run("deleted video cannot be saved for later", func(t *testing.T) {
		deletedAt := time.Now()
		repo := &fakeVideoStateRepo{
			video: &repository.Video{ID: 7, Status: repository.VideoStatusDone, DeletedAt: &deletedAt},
		}
		h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}

		_, err := h.SetWatchLater(ctx, SetWatchLaterInput{VideoID: 7, WatchLater: true})
		assertTRPCCode(t, err, trpcgo.CodeBadRequest)
		if repo.setCalls != 0 {
			t.Fatalf("SetVideoWatchLater called %d times, want 0", repo.setCalls)
		}
	})

	t.Run("progress requires a done video without pre-reading it", func(t *testing.T) {
		repo := &fakeVideoStateRepo{
			video: &repository.Video{ID: 7, Status: repository.VideoStatusRunning},
		}
		h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}

		_, err := h.UpdateWatchProgress(ctx, UpdateWatchProgressInput{
			VideoID:         7,
			PositionSeconds: 12,
			ObservedAtMs:    1234,
		})
		assertTRPCCode(t, err, trpcgo.CodeNotFound)
		if repo.getCalls != 0 || repo.progressCalls != 0 {
			t.Fatalf("get/progress calls = %d/%d, want 0/0", repo.getCalls, repo.progressCalls)
		}
	})

	t.Run("done video can update state", func(t *testing.T) {
		repo := &fakeVideoStateRepo{
			video: &repository.Video{ID: 7, Status: repository.VideoStatusDone},
		}
		h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}

		out, err := h.SetWatchLater(ctx, SetWatchLaterInput{VideoID: 7, WatchLater: true})
		if err != nil {
			t.Fatalf("SetWatchLater returned error: %v", err)
		}
		if !out.WatchLater || repo.setCalls != 1 {
			t.Fatalf("watch later state = %+v, calls = %d", out, repo.setCalls)
		}

		progress, err := h.UpdateWatchProgress(ctx, UpdateWatchProgressInput{
			VideoID:         7,
			PositionSeconds: 12.5,
			Completed:       true,
			ObservedAtMs:    5678,
		})
		if err != nil {
			t.Fatalf("UpdateWatchProgress returned error: %v", err)
		}
		if progress.LastPositionSeconds != 12.5 || progress.CompletedAt == nil || repo.progressCalls != 1 || repo.observedAtMs != 5678 {
			t.Fatalf("progress state = %+v, calls = %d", progress, repo.progressCalls)
		}
	})
}

func assertTRPCCode(t *testing.T, err error, want trpcgo.ErrorCode) {
	t.Helper()
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("err = %T (%v), want *trpcgo.Error", err, err)
	}
	if te.Code != want {
		t.Fatalf("code = %v, want %v (msg %q)", te.Code, want, te.Message)
	}
}
