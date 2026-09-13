package video

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

func TestRemovedSingleFileStreamRoute(t *testing.T) {
	srv := sessionPartTestServer(t, &signedRepo{video: doneVideo()})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+"/api/v1/videos/42/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s obsolete stream route = %d", method, resp.StatusCode)
		}
	}
}

func TestStreamShutdownCancelsAndJoinsOwnedWaveform(t *testing.T) {
	h := &StreamHandler{waveformFlights: newWaveformFlights()}
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := h.waveformFlights.Do(t.Context(), "recording", func(ctx context.Context) (AudioWaveformResponse, int, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release // An already-started publication must join before shutdown returns.
			return AudioWaveformResponse{}, http.StatusInternalServerError, ctx.Err()
		})
		done <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := h.waveformFlights.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unfinished shutdown = %v", err)
	}
	<-cancelled
	closed := make(chan error, 1)
	go func() { closed <- h.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("handler reported joined before publication finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("owned build cancellation = %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	_, _, err := h.waveformFlights.Do(t.Context(), "late", func(context.Context) (AudioWaveformResponse, int, error) {
		t.Error("shutdown accepted a new flight")
		return AudioWaveformResponse{}, http.StatusOK, nil
	})
	if !errors.Is(err, background.ErrStopped) {
		t.Fatalf("late admission = %v", err)
	}
}

type waveformBackendFault struct {
	storage.Storage
	phase string
	err   error
	hits  int
}

func (s *waveformBackendFault) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	if s.err != nil && ((s.phase == "load" && strings.HasSuffix(key, "-waveform.json")) || (s.phase == "materialize" && strings.HasSuffix(key, ".m4a"))) {
		s.hits++
		return nil, s.err
	}
	return s.Storage.Open(ctx, key)
}
func (s *waveformBackendFault) Save(ctx context.Context, key string, r io.Reader) error {
	if s.err != nil && s.phase == "publish" && strings.HasSuffix(key, "-waveform.json") {
		s.hits++
		return s.err
	}
	return s.Storage.Save(ctx, key, r)
}

func TestWaveformStorageFailuresAfterReadinessReturn503(t *testing.T) {
	for _, phase := range []string{"load", "materialize", "publish"} {
		for _, verdict := range []error{storage.ErrUnattached, storage.ErrUnreachable, storage.ErrReadOnly, storage.ErrFull} {
			t.Run(phase+"/"+verdict.Error(), func(t *testing.T) {
				v := doneVideo()
				v.RecordingType = repository.RecordingTypeAudio
				repo := &signedRepo{video: v, parts: []repository.VideoPart{{PartIndex: 1, Filename: "audio.m4a", DurationSeconds: 2, SizeBytes: 5}}}
				backend := &waveformBackendFault{Storage: &signedStorage{bodies: map[string][]byte{"videos/audio.m4a": []byte("audio")}}, phase: phase}
				h := NewStreamHandler(repo, streamMedia(t, repo, backend, nil, nil), nil, testClientLogger(), WithWaveformGenerator(&fakeWaveformGenerator{}))
				t.Cleanup(func() {
					if err := h.Close(); err != nil {
						t.Error(err)
					}
				})
				if phase == "load" {
					if _, status, err := h.audioWaveform(t.Context(), v.ID); err != nil || status != http.StatusOK {
						t.Fatalf("seed artifact: %d, %v", status, err)
					}
				}
				backend.err = verdict // Cached readiness remains healthy; the actual operation fails.
				_, status, err := h.audioWaveform(t.Context(), v.ID)
				if status != http.StatusServiceUnavailable || !errors.Is(err, verdict) || backend.hits == 0 {
					t.Fatalf("storage failure: status=%d err=%v calls=%d", status, err, backend.hits)
				}
			})
		}
	}
}
