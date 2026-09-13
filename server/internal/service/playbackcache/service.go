package playbackcache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

const (
	// defaultBuildTimeout limits ffmpeg work; interrupted builds retry on the next play.
	defaultBuildTimeout = 6 * time.Hour
	// Container framing and faststart can grow concat output beyond the part sizes.
	buildOvershootMarginDivisor = 64
	// Serial concat avoids competing full-recording reads and rewrites.
	defaultBuildConcurrency = 1
	// Keep one twentieth of the local filesystem free for recording writes.
	diskReserveFraction = 20
)

// Runner stream-copies the concat list into an atomic, seekable output file.
type Runner interface {
	Concat(ctx context.Context, listPath, outputPath string) error
}

type Option func(*Service)

type Service struct {
	repo   repository.Repository
	store  *mediastore.Store
	runner Runner
	log    *slog.Logger

	buildTimeout time.Duration

	// fsStat returns total and available bytes for the local media filesystem.
	fsStat func(root string) (total, avail int64, err error)
	// capacityOverride replaces the storage-derived budget when non-nil.
	capacityOverride func(current int64) (int64, bool)

	work *background.Runner
}

func New(repo repository.Repository, store *mediastore.Store, ffmpegPath string, log *slog.Logger, opts ...Option) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With("domain", "playback-cache")
	s := &Service{
		repo:         repo,
		store:        store,
		runner:       remuxRunner{remuxer: &remux.Remuxer{FFmpegPath: ffmpegPath, Log: log}},
		log:          log,
		buildTimeout: defaultBuildTimeout,
		fsStat:       statfsBytes,
		work:         background.New(map[string]int{"playback": defaultBuildConcurrency}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) SetRunner(r Runner) {
	if r != nil {
		s.runner = r
	}
}

func (s *Service) storageReady(ctx context.Context) error {
	return s.store.Verify(ctx)
}

// storageDeletionReady keeps reclamation available when writes exhaust capacity.
// Like publication, deletion verifies identity at the operation boundary.
func (s *Service) storageDeletionReady(ctx context.Context) error {
	err := s.storageReady(ctx)
	if storage.CanDelete(err) {
		return nil
	}
	return err
}

// StartBuild queues a build independently of the triggering HTTP request.
// Shutdown cancels builds; interrupted recordings retry on their next play.
func (s *Service) StartBuild(_ context.Context, videoID int64) error {
	if s == nil || s.repo == nil || s.store == nil || s.runner == nil {
		return fmt.Errorf("playback builder unavailable")
	}
	err := s.work.StartQueued("playback", fmt.Sprint(videoID), func(parent context.Context) error {
		ctx, cancel := context.WithTimeout(parent, s.buildTimeout)
		defer cancel()
		return s.BuildNow(ctx, videoID)
	}, func(err error) {
		if err != nil {
			s.log.Warn("playback artifact build failed", "video_id", videoID, "error", err)
		}
	})
	if errors.Is(err, background.ErrBusy) {
		return nil
	}
	return err
}

func (s *Service) Wait() {
	if s != nil {
		_ = s.work.WaitIdle(context.Background())
	}
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.work.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.work.Wait(ctx); err != nil {
		s.log.Warn("playback cache shutdown: build still unwinding", "error", err)
	}
}

// Reconcile prunes an enabled cache to its current budget without starting builds.
// Disabling the cache preserves existing artifacts.
func (s *Service) Reconcile(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	settings, err := playbackSettings(ctx, s.repo)
	if err != nil {
		return err
	}
	if !settings.active() {
		return nil
	}
	return s.pruneWithSettings(ctx, settings)
}

type playbackConfig struct {
	enabled      bool
	autoGenerate bool
	maxPercent   int
}

// active reports whether cache builds and pruning are enabled.
func (c playbackConfig) active() bool {
	return c.enabled && c.maxPercent > 0
}

func playbackSettings(ctx context.Context, repo repository.Repository) (playbackConfig, error) {
	settings, err := repo.GetServerSettings(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return playbackConfig{}, nil
		}
		return playbackConfig{}, err
	}
	cfg := playbackConfig{
		enabled:      settings.PlaybackCacheEnabled,
		autoGenerate: settings.PlaybackCacheAutoGenerate,
		maxPercent:   settings.PlaybackCacheMaxPercent,
	}
	if cfg.maxPercent < 0 {
		cfg.maxPercent = 0
	}
	if cfg.maxPercent > 100 {
		cfg.maxPercent = 100
	}
	return cfg, nil
}
