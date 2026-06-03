package videorequest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// fakeReqRepo satisfies repository.Repository by embedding it (unused methods
// panic) and overrides the two Request touches.
type fakeReqRepo struct {
	repository.Repository
	video    *repository.Video
	getErr   error
	addErr   error
	added    bool
	addedVID int64
	addedUID string
}

func (f *fakeReqRepo) GetVideo(_ context.Context, _ int64) (*repository.Video, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.video, nil
}

func (f *fakeReqRepo) AddVideoRequest(_ context.Context, videoID int64, userID string) error {
	f.added = true
	f.addedVID, f.addedUID = videoID, userID
	return f.addErr
}

func newTestService(repo repository.Repository) *Service {
	return New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// A non-existent video must surface ErrNotFound (→ 404) and must NOT attempt the
// insert, instead of letting the foreign-key violation surface as a 500.
func TestRequest_MissingVideoReturnsNotFound(t *testing.T) {
	repo := &fakeReqRepo{getErr: repository.ErrNotFound}
	err := newTestService(repo).Request(context.Background(), "user-1", 42)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("Request err = %v, want ErrNotFound", err)
	}
	if repo.added {
		t.Fatal("AddVideoRequest must not run when the video does not exist")
	}
}

func TestRequest_ExistingVideoIsAdded(t *testing.T) {
	repo := &fakeReqRepo{video: &repository.Video{ID: 42}}
	if err := newTestService(repo).Request(context.Background(), "user-1", 42); err != nil {
		t.Fatalf("Request err = %v, want nil", err)
	}
	if !repo.added || repo.addedVID != 42 || repo.addedUID != "user-1" {
		t.Fatalf("AddVideoRequest called wrong: added=%v vid=%d uid=%q", repo.added, repo.addedVID, repo.addedUID)
	}
}

func TestRequest_PropagatesAddError(t *testing.T) {
	wantErr := errors.New("insert conflict")
	repo := &fakeReqRepo{video: &repository.Video{ID: 42}, addErr: wantErr}
	if err := newTestService(repo).Request(context.Background(), "user-1", 42); !errors.Is(err, wantErr) {
		t.Fatalf("Request err = %v, want %v", err, wantErr)
	}
	if !repo.added {
		t.Fatal("error must come from AddVideoRequest having been attempted")
	}
}
