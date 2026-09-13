package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type shutdownWaveformInput struct {
	*storage.LocalStorage
	started, cancelled, release chan struct{}
}

func (s *shutdownWaveformInput) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	if key == "videos/audio.m4a" {
		close(s.started)
		<-ctx.Done()
		close(s.cancelled)
		<-s.release
		return nil, ctx.Err()
	}
	return s.LocalStorage.Open(ctx, key)
}

func TestRouterShutdownCancelsAndJoinsHTTPWaveformGeneration(t *testing.T) {
	ctx := t.Context()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.DiscardHandler)
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &shutdownWaveformInput{LocalStorage: raw, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	cfg := &config.Config{Env: config.Environment{ScratchDir: t.TempDir()}, App: config.AppConfig{Storage: config.StorageConfig{Type: "local", LocalPath: raw.Root}}}
	bus := eventbus.New()
	recordings := NewRecordingServices(cfg, repo, backend, bus, log)
	if _, err := recordings.StorageHealth.Attach(ctx); err != nil {
		t.Fatal(err)
	}
	if err := raw.Save(ctx, "videos/audio.m4a", strings.NewReader("audio")); err != nil {
		t.Fatal(err)
	}
	mgr, err := session.NewManager(repo, "waveform-shutdown-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatal(err)
	}
	cookie := mintSessionCookie(t, repo, mgr, "waveform-viewer", "viewer")
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "audio", BroadcasterLogin: "audio", BroadcasterName: "Audio"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "audio", Filename: "audio", BroadcasterID: "audio", Status: repository.VideoStatusDone, RecordingType: repository.RecordingTypeAudio})
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: v.ID, PartIndex: 1, Filename: "audio.m4a", Quality: "audio_only", Codec: repository.CodecAAC, SegmentFormat: repository.SegmentFormatFMP4})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{ID: p.ID, DurationSeconds: 2, SizeBytes: 5}); err != nil {
		t.Fatal(err)
	}
	router, closeRouter := SetupRouter(cfg, repo, mgr, nil, backend, nil, nil, bus, nil, nil, nil, log, recordings)
	srv := httptest.NewServer(router)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(backend.release) }); _ = closeRouter(); srv.Close() })
	done := make(chan error, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/videos/%d/waveform", srv.URL, v.ID), nil)
		if err != nil {
			done <- err
			return
		}
		req.AddCookie(cookie)
		resp, err := srv.Client().Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				err = errors.New("shutdown published a successful waveform")
			}
		}
		done <- err
	}()
	select {
	case <-backend.started:
	case err := <-done:
		t.Fatalf("request did not reach generation: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("waveform did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- closeRouter() }()
	select {
	case <-backend.cancelled:
	case <-time.After(time.Second):
		t.Fatal("router shutdown did not cancel waveform")
	}
	select {
	case err := <-closed:
		t.Fatalf("router returned before owned I/O joined: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	release.Do(func() { close(backend.release) })
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetVideoWaveformKey(ctx, v.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("shutdown published reference: %v", err)
	}
}
