package video

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/trpcgo"
)

type failingUserStateRepo struct {
	repository.Repository
	readErr error
}

func (r *failingUserStateRepo) GetVideoUserState(ctx context.Context, userID string, videoID int64) (*repository.VideoUserState, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	return r.Repository.GetVideoUserState(ctx, userID, videoID)
}

func (r *failingUserStateRepo) ListVideoUserStatesForVideos(ctx context.Context, userID string, ids []int64) ([]repository.VideoUserState, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	return r.Repository.ListVideoUserStatesForVideos(ctx, userID, ids)
}

func TestVideoSnapshotsRequireSavedProgress(t *testing.T) {
	ctx := middleware.WithUser(t.Context(), &repository.User{ID: "resume-viewer"})
	repo := &failingUserStateRepo{Repository: sqliteadapter.New(testdb.NewSQLiteDB(t))}
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "resume-viewer", Login: "resume-viewer", DisplayName: "Viewer", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "resume-channel", BroadcasterLogin: "resume-channel", BroadcasterName: "Channel"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "resume-progress", Filename: "resume-progress", BroadcasterID: "resume-channel", Status: repository.VideoStatusDone, Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkVideoDone(ctx, v.ID, 1000, 100, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.UpdateVideoWatchProgress(ctx, "resume-viewer", v.ID, 120, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}
	for _, tc := range []struct {
		name string
		load func() ([]VideoResponse, error)
	}{
		{"detail", func() ([]VideoResponse, error) {
			row, err := h.GetByID(ctx, GetByIDInput{ID: v.ID})
			return []VideoResponse{row}, err
		}},
		{"list", func() ([]VideoResponse, error) { return h.List(ctx, ListInput{}) }},
		{"continue_watching_page", func() ([]VideoResponse, error) {
			page, err := h.ListPage(ctx, ListPageInput{ContinueWatchingOnly: true, Sort: "last_watched"})
			return page.Items, err
		}},
		{"continue_watching_shelf", func() ([]VideoResponse, error) { return h.ContinueWatching(ctx, ContinueWatchingInput{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo.readErr = errors.New("progress storage unavailable")
			_, err := tc.load()
			assertTRPCCode(t, err, trpcgo.CodeInternalServerError)
			repo.readErr = nil
			rows, err := tc.load()
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].UserState == nil || rows[0].UserState.LastPositionSeconds != 120 || rows[0].UserState.ProgressRevision != saved.ProgressRevision {
				t.Fatalf("retry lost saved resume position: %+v", rows)
			}
		})
	}
}
