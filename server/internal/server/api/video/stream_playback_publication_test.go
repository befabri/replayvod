package video

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/go-chi/chi/v5"
)

type playbackStatBarrier struct {
	storage.Storage
	afterMissingStat func(context.Context)
	beforeOpen       func(context.Context) error
}

func (s *playbackStatBarrier) Stat(ctx context.Context, path string) (storage.FileInfo, error) {
	info, err := s.Storage.Stat(ctx, path)
	if errors.Is(err, fs.ErrNotExist) && s.afterMissingStat != nil {
		s.afterMissingStat(ctx)
	}
	return info, err
}

func (s *playbackStatBarrier) Open(ctx context.Context, path string) (io.ReadSeekCloser, error) {
	if s.beforeOpen != nil {
		if err := s.beforeOpen(ctx); err != nil {
			return nil, err
		}
	}
	return s.Storage.Open(ctx, path)
}

func playbackPublicationFixture(t *testing.T) (repository.Repository, *storage.LocalStorage, *storagehealth.Monitor, *repository.Video, *repository.VideoPlaybackAssetInput) {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	monitor := storagehealth.New(repo, store, nil, testClientLogger(), "local", store.Root)
	if _, err := monitor.Attach(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChannel(t.Context(), &repository.Channel{BroadcasterID: "playback", BroadcasterLogin: "playback", BroadcasterName: "Playback"}); err != nil {
		t.Fatal(err)
	}
	video, err := repo.CreateVideo(t.Context(), &repository.VideoInput{JobID: "playback-publish", Filename: "playback", DisplayName: "Playback", BroadcasterID: "playback", Status: repository.VideoStatusDone, Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo})
	if err != nil {
		t.Fatal(err)
	}
	filename, mime := "old-playback.mp4", "video/mp4"
	size, duration := int64(8), float64(2)
	generated := time.Now().Add(-time.Hour)
	input := &repository.VideoPlaybackAssetInput{VideoID: video.ID, Status: repository.PlaybackAssetStatusReady, Filename: &filename, MimeType: &mime, SizeBytes: &size, DurationSeconds: &duration, GeneratedAt: &generated, LastAccessedAt: &generated}
	if _, err := repo.UpsertVideoPlaybackAsset(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	return repo, store, monitor, video, input
}

type playbackHTTPResult struct {
	status int
	body   string
	err    error
}

func requestPlayback(url string) playbackHTTPResult {
	resp, err := http.Get(url)
	if err != nil {
		return playbackHTTPResult{err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return playbackHTTPResult{status: resp.StatusCode, body: string(body), err: err}
}

func waitPlaybackResult(t *testing.T, done <-chan playbackHTTPResult) playbackHTTPResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("playback request did not finish")
		return playbackHTTPResult{}
	}
}

func TestMissingPlaybackCannotDiscardPublishedReplacement(t *testing.T) {
	for _, mode := range []string{"same filename", "changed filename", "building"} {
		t.Run(mode, func(t *testing.T) {
			repo, local, monitor, video, next := playbackPublicationFixture(t)
			locks := &recordinglock.Locks{}
			missing, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var captured atomic.Bool
			store := &playbackStatBarrier{Storage: local, afterMissingStat: func(ctx context.Context) {
				if captured.CompareAndSwap(false, true) {
					close(missing)
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}}
			store.beforeOpen = func(ctx context.Context) error {
				// Streaming must run after the reconciliation lock is released.
				ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
				unlock, err := locks.Lock(ctx, video.ID)
				if err == nil {
					unlock()
				}
				return err
			}
			srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithStorageGate(monitor), WithRecordingLocks(locks))
			done, finished := make(chan playbackHTTPResult, 1), make(chan struct{})
			go func() {
				defer close(finished)
				done <- requestPlayback(fmt.Sprintf("%s/api/v1/videos/%d/playback/stream", srv.URL, video.ID))
			}()
			t.Cleanup(func() {
				unblock()
				srv.CloseClientConnections()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Error("playback request did not exit")
				}
			})
			select {
			case <-missing:
			case result := <-done:
				t.Fatalf("request skipped missing-file barrier: %+v", result)
			case <-time.After(3 * time.Second):
				t.Fatal("request did not check the old missing artifact")
			}
			// Publish while the HTTP request still holds the old not-found result.
			// This is the real Save/upsert sequence under publication ownership,
			// with actual SQLite rows and local bytes, without a synthetic row mock.
			publishCtx, cancelPublish := context.WithTimeout(t.Context(), time.Second)
			defer cancelPublish()
			unlock, err := locks.Lock(publishCtx, video.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "changed filename" {
				filename := "replacement-playback.mp4"
				next.Filename = &filename
			}
			if mode == "building" {
				next.Status = repository.PlaybackAssetStatusBuilding
				next.Filename, next.MimeType, next.LastAccessedAt = nil, nil, nil
			} else if err := local.Save(t.Context(), storagekeys.Video(*next.Filename), strings.NewReader("replacement")); err != nil {
				unlock()
				t.Fatal(err)
			}
			if mode != "building" {
				size := int64(len("replacement"))
				next.SizeBytes = &size
			}
			generated := time.Now()
			next.GeneratedAt = &generated
			_, err = repo.UpsertVideoPlaybackAsset(t.Context(), next)
			unlock()
			if err != nil {
				t.Fatal(err)
			}
			unblock()
			result := waitPlaybackResult(t, done)
			current, err := repo.GetVideoPlaybackAsset(t.Context(), video.ID)
			if err != nil || current.Status != next.Status {
				t.Fatalf("stale missing-file result discarded replacement: asset=%+v err=%v response=%+v", current, err, result)
			}
			if mode != "building" && (current.Filename == nil || *current.Filename != *next.Filename) {
				t.Fatalf("replacement filename = %v, want %s", current.Filename, *next.Filename)
			}
			wantStatus := http.StatusOK
			if mode == "building" {
				wantStatus = http.StatusNotFound
			}
			if result.err != nil || result.status != wantStatus || (mode != "building" && result.body != "replacement") {
				t.Fatalf("replacement playback = %+v, want status %d", result, wantStatus)
			}
		})
	}
}

type playbackDemotionBarrier struct {
	repository.Repository
	deleting chan struct{}
	release  chan struct{}
}

func (r *playbackDemotionBarrier) DeleteVideoPlaybackAsset(ctx context.Context, id int64) error {
	close(r.deleting)
	select {
	case <-r.release:
		return r.Repository.DeleteVideoPlaybackAsset(ctx, id)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMissingPlaybackDemotionOwnsRecordingUntilDeleteCompletes(t *testing.T) {
	repo, store, monitor, video, _ := playbackPublicationFixture(t)
	locks := &recordinglock.Locks{}
	barrier := &playbackDemotionBarrier{Repository: repo, deleting: make(chan struct{}), release: make(chan struct{})}
	unblock := sync.OnceFunc(func() { close(barrier.release) })
	srv := streamRouteTestServer(t, barrier, store, testClientLogger(), WithStorageGate(monitor), WithRecordingLocks(locks))
	done, finished := make(chan playbackHTTPResult, 1), make(chan struct{})
	go func() {
		defer close(finished)
		done <- requestPlayback(fmt.Sprintf("%s/api/v1/videos/%d/playback/stream", srv.URL, video.ID))
	}()
	t.Cleanup(func() {
		unblock()
		srv.CloseClientConnections()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("demotion request did not exit")
		}
	})
	select {
	case <-barrier.deleting:
	case result := <-done:
		t.Fatalf("request did not reach metadata deletion: %+v", result)
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach metadata deletion")
	}
	// Pause before SQLite executes DELETE: database transaction locks cannot
	// accidentally protect this window. A publisher needs recording ownership.
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	unlock, err := locks.Lock(ctx, video.ID)
	if err == nil {
		unlock()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("publication acquired ownership during cache demotion: %v", err)
	}
	unblock()
	result := waitPlaybackResult(t, done)
	if result.err != nil || result.status != http.StatusNotFound {
		t.Fatalf("missing playback = %+v", result)
	}
	if current, err := repo.GetVideoPlaybackAsset(t.Context(), video.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("confirmed missing asset was not demoted: %+v, %v", current, err)
	}
}

func TestMissingPlaybackCancellationDoesNotWaitForPublisher(t *testing.T) {
	repo, local, monitor, video, _ := playbackPublicationFixture(t)
	locks := &recordinglock.Locks{}
	unlock, err := locks.Lock(t.Context(), video.ID)
	if err != nil {
		t.Fatal(err)
	}
	releaseOwnership := sync.OnceFunc(unlock)
	defer releaseOwnership()
	missing := make(chan struct{})
	observed := sync.OnceFunc(func() { close(missing) })
	store := &playbackStatBarrier{Storage: local, afterMissingStat: func(context.Context) { observed() }}
	h := NewStreamHandler(repo, store, nil, testClientLogger(), WithStorageGate(monitor), WithRecordingLocks(locks))
	router := chi.NewRouter()
	h.SetupRoutes(router, func(next http.Handler) http.Handler { return next })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/videos/%d/playback/stream", video.ID), nil).WithContext(ctx)
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		router.ServeHTTP(response, req)
	}()
	t.Cleanup(func() {
		cancel()
		releaseOwnership()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("cancelled handler did not exit during cleanup")
		}
	})
	select {
	case <-missing:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach the missing artifact")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancelled HTTP handler waited for publisher ownership")
	}
	if response.Code != statusClientClosed {
		t.Fatalf("cancelled response = %d, want %d", response.Code, statusClientClosed)
	}
	if current, err := repo.GetVideoPlaybackAsset(t.Context(), video.ID); err != nil || current.Status != repository.PlaybackAssetStatusReady {
		t.Fatalf("cancelled request demoted the asset: %+v, %v", current, err)
	}
}
