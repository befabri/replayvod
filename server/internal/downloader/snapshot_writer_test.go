package downloader

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/storage"
)

// fakeStorage is a minimal storage.Storage test double — only the
// Save method is exercised by storageSnapshotWriter, so the other
// methods exist as no-ops for interface compliance. Tracks each
// Save call by path so the test can assert the exact path
// template without pulling in the real LocalStorage backend.
type fakeStorage struct {
	mu       sync.Mutex
	saves    map[string][]byte
	saveErrs map[string]error // per-path override; nil = success
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{saves: map[string][]byte{}}
}

func (f *fakeStorage) Save(_ context.Context, path string, r io.Reader) error {
	if err, ok := f.saveErrs[path]; ok && err != nil {
		return err
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves[path] = buf
	return nil
}

// The other Storage methods are never called by
// storageSnapshotWriter; stubbing satisfies the interface without
// shipping a full fake.
func (f *fakeStorage) Open(_ context.Context, _ string) (io.ReadSeekCloser, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStorage) Delete(_ context.Context, _ string) error { return errors.New("not impl") }
func (f *fakeStorage) Exists(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.saves[key]
	return ok, nil
}
func (f *fakeStorage) Stat(_ context.Context, _ string) (storage.FileInfo, error) {
	return storage.FileInfo{}, errors.New("not impl")
}

func snapshotWriterFixture(t *testing.T, raw storage.Storage, name string) (*storageSnapshotWriter, repository.Repository) {
	t.Helper()
	svc := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, svc, "snapshots")
	svc.storage = mediatest.New(t, svc.repo, raw, nil, nil)
	return &storageSnapshotWriter{storage: svc.storage, claim: d.claim(), filename: name}, svc.repo
}

func TestStorageSnapshotWriter_PathTemplate(t *testing.T) {
	fs := newFakeStorage()
	w, repo := snapshotWriterFixture(t, fs, "recording")
	for _, i := range []int{0, 1, 9, 10, 47, 99} {
		if err := w.WriteSnapshot(t.Context(), i, strings.NewReader("frame")); err != nil {
			t.Fatal(err)
		}
		if _, ok := fs.saves[storagekeys.Snapshot("recording", i)]; !ok {
			t.Fatalf("missing snapshot %d", i)
		}
	}
	video, err := repo.GetVideo(t.Context(), w.claim.VideoID)
	if err != nil || video.Thumbnail == nil || *video.Thumbnail != storagekeys.Snapshot("recording", 0) {
		t.Fatalf("first snapshot promotion: %+v %v", video, err)
	}
}
func TestStorageSnapshotWriter_PropagatesStorageError(t *testing.T) {
	fs := newFakeStorage()
	want := errors.New("disk full")
	fs.saveErrs = map[string]error{storagekeys.Snapshot("rec", 0): want}
	w, repo := snapshotWriterFixture(t, fs, "rec")
	if err := w.WriteSnapshot(t.Context(), 0, strings.NewReader("frame")); !errors.Is(err, want) {
		t.Fatal(err)
	}
	v, err := repo.GetVideo(t.Context(), w.claim.VideoID)
	if err != nil || v.Thumbnail != nil {
		t.Fatalf("failed snapshot promoted: %+v %v", v, err)
	}
}
func TestStorageSnapshotWriter_CancellationPreventsPublication(t *testing.T) {
	fs := newFakeStorage()
	w, _ := snapshotWriterFixture(t, fs, "rec")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := w.WriteSnapshot(ctx, 0, strings.NewReader("frame")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(fs.saves) != 0 {
		t.Fatal("cancelled child published a snapshot")
	}
}
func TestStorageSnapshotWriter_StoppedAttemptCannotPromote(t *testing.T) {
	fs := newFakeStorage()
	w, repo := snapshotWriterFixture(t, fs, "rec")
	if err := repo.UpdateVideoStatus(t.Context(), w.claim.VideoID, repository.VideoStatusDone); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSnapshot(t.Context(), 0, strings.NewReader("frame")); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatal(err)
	}
	if len(fs.saves) != 0 {
		t.Fatal("terminal attempt published a snapshot")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A lost upload response leaves the old physical key owned indefinitely. A
// resumed sampler can publish a different frame without the late upload
// overwriting it or promoting the failed first frame to the video thumbnail.
func TestSnapshotResumeSkipsUnresolvedAndPublishedFrames(t *testing.T) {
	fs := newFakeStorage()
	firstKey, nextKey := storagekeys.Snapshot("rec", 0), storagekeys.Snapshot("rec", 1)
	fs.saveErrs = map[string]error{firstKey: context.DeadlineExceeded}
	w, repo := snapshotWriterFixture(t, fs, "rec")
	if err := w.WriteSnapshot(t.Context(), 0, strings.NewReader("old frame")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	resumed := &storageSnapshotWriter{storage: w.storage, claim: w.claim, filename: w.filename}
	if err := resumed.WriteSnapshot(t.Context(), 0, strings.NewReader("new frame")); err != nil {
		t.Fatal(err)
	}
	delete(fs.saveErrs, firstKey)
	if err := fs.Save(t.Context(), firstKey, strings.NewReader("old frame")); err != nil {
		t.Fatal(err)
	}
	if string(fs.saves[nextKey]) != "new frame" {
		t.Fatal("late upload overwrote a newer snapshot")
	}
	v, err := repo.GetVideo(t.Context(), w.claim.VideoID)
	if err != nil || v.Thumbnail == nil || *v.Thumbnail != nextKey {
		t.Fatalf("thumbnail=%+v %v", v, err)
	}
	pending, err := repo.GetMediaPublication(t.Context(), firstKey)
	if err != nil || !pending.Unresolved {
		t.Fatalf("unknown upload retired: %+v %v", pending, err)
	}
	resumedAgain := &storageSnapshotWriter{storage: w.storage, claim: w.claim, filename: w.filename}
	if err := resumedAgain.WriteSnapshot(t.Context(), 0, strings.NewReader("third frame")); err != nil {
		t.Fatal(err)
	}
	if string(fs.saves[storagekeys.Snapshot("rec", 2)]) != "third frame" {
		t.Fatal("restart reused a published frame key")
	}
}
