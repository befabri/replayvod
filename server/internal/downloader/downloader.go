// Package downloader records Twitch broadcasts and archives as recoverable media parts.
// Each recording has a job ID for progress subscriptions and durable cancellation.
// Shutdown preserves unfinished attempts and scratch for Resume.
package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/downloader/probe"
	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/downloader/thumbnail"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/archiveposter"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	provider "github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/internal/waveform"
)

func qualityToHeight(q string) string {
	switch q {
	case repository.Quality1440:
		return "1440"
	case repository.QualityBest:
		return "best"
	case repository.QualityHigh:
		return "1080"
	case repository.QualityMedium:
		return "720"
	case repository.QualityLow:
		return "480"
	default:
		return "1080"
	}
}

func (p Params) qualityCap() string {
	if p.MaxHeight > 0 {
		return strconv.Itoa(p.MaxHeight)
	}
	return qualityToHeight(p.Quality)
}

// Params describes a recording request; zero codec settings select video without overrides.
type Params struct {
	StreamStartedAt  time.Time
	BroadcasterID    string
	BroadcasterLogin string
	DisplayName      string
	// Title is the opening stream title; empty means no title was observed.
	Title string
	// CategoryID is the opening Twitch game ID; empty means no category was observed.
	CategoryID string
	// CategoryName may be empty to preserve an existing category name during enrichment.
	CategoryName string
	Quality      string
	Language     string
	ViewerCount  int64
	StreamID     *string

	// MaxHeight pins the recording to an exact rendition height taken from
	// LiveRenditions. Zero leaves the cap to Quality. Video only.
	MaxHeight int

	// RecordingType is "video" or "audio"; empty defaults to video.
	RecordingType string

	ForceH264 bool

	// TriggerScheduleID and retention fields are nil for manual recordings, which must
	// not inherit deletion policies from later schedule edits.
	TriggerScheduleID         *int64
	RetentionSourceScheduleID *int64
	RetentionWindowHours      *int64

	// VODID selects an archive without live metadata observers; BroadcastAt is its original air date.
	VODID       string
	BroadcastAt *time.Time
	// PosterURL is the VOD's thumbnail on Twitch. It is fetched when the
	// archive starts and is the only poster an audio archive receives.
	PosterURL string
}

func (p Params) isVOD() bool { return p.VODID != "" }

// Progress is a cumulative recording snapshot; a newer snapshot supersedes all earlier ones.
// Intermediate snapshots may be dropped, and closing the progress channel ends delivery.
type Progress struct {
	JobID string `json:"job_id"`

	// PartIndex is 1-based and increments at each output part boundary.
	PartIndex int `json:"part_index"`

	// Stage labels the active pipeline stage. Values:
	//   "auth" | "playlist" | "segments" | "remux" |
	//   "metadata" | "thumbnail" | "done"
	Stage string `json:"stage"`

	// BytesWritten includes finalized output sizes and current committed source bytes;
	// remuxing can raise or lower this estimate at a part boundary.
	BytesWritten int64 `json:"bytes_written"`

	// SegmentsTotal is -1 until a live playlist closes; ad gaps do not count toward gap policy.
	SegmentsDone   int64 `json:"segments_done"`
	SegmentsGaps   int64 `json:"segments_gaps"`
	SegmentsAdGaps int64 `json:"segments_ad_gaps"`
	SegmentsTotal  int64 `json:"segments_total"`

	// Percent is -1 while the total is unknown.
	Percent float64 `json:"percent"`

	// Speed is a formatted bytes-per-second rate, or empty until enough samples exist.
	Speed string `json:"speed"`

	// ETA is a formatted duration, or empty when total or speed is unknown.
	ETA string `json:"eta"`

	Quality string   `json:"quality"`
	FPS     *float64 `json:"fps,omitempty"`
	Codec   string   `json:"codec"`

	// RecordingType is "video" or "audio".
	RecordingType string `json:"recording_type"`

	// MediaOffsetSeconds is nil while playback position is provisional.
	MediaOffsetSeconds *float64 `json:"media_offset_seconds,omitempty"`
}

// Service manages recording attempts and their recovery.
// Create one with NewService; its recording methods are safe for concurrent use.
type Service struct {
	manual  map[string]*manualRun
	now     func() time.Time
	observe func(context.Context, string) (*provider.Stream, error)
	cfg     *config.Config
	repo    repository.Repository
	storage *mediastore.Store
	log     *slog.Logger

	twitch              *twitch.Client
	fetcher             *hls.Fetcher
	remuxer             *remux.Remuxer
	probe               *probe.Probe
	thumb               *thumbnail.Generator
	snapshots           *thumbnail.Snapshotter
	waveforms           waveform.Generator
	playbackCredentials PlaybackCredentials
	hydrator            *streammeta.Hydrator
	metaWatcher         titleWatcher
	channelSubs         ChannelUpdateSubscriber

	mu     sync.Mutex
	active map[string]*download
	work   *background.Runner
	// pumpMu serializes PumpArchiveQueue and DequeueArchive so a queued
	// archive is started or removed by exactly one caller.
	pumpMu          sync.Mutex
	activeSubs      map[int]chan struct{}
	nextActiveSubID int

	// discoveryWG joins the polling loop. Recording writers and settlement
	// belong to work and its scopes.
	discoveryWG sync.WaitGroup

	shuttingDown atomic.Bool

	bus *eventbus.Buses

	posters *archiveposter.Store

	// retryCancel is set once under mu, and cancelled by Shutdown. The loop
	// belongs to discoveryWG; it never owns recording writers.
	retryCancel   context.CancelFunc
	retryInterval time.Duration
}

type download struct {
	captureIdentityVerified bool
	recovered               bool
	manual                  *manualRun
	reservation             *background.Reservation
	workspace               *mediastore.Workspace
	executionID             string
	previousExecutionID     string
	runCtx                  context.Context
	stopChildren            func()
	persistenceErr          error
	jobID                   string
	videoID                 int64
	broadcasterID           string
	vod                     bool
	attempt                 int32
	// limiter paces an archive's segment bytes; nil for live recordings.
	limiter        hls.RateLimiter
	cancel         context.CancelFunc
	userCancelled  bool
	progressCh     chan Progress
	startedAt      time.Time
	progressMu     sync.RWMutex
	latestProgress Progress

	resume *ResumeState

	videoPartID int64

	// completedMediaDurationSeconds uses probed durations for finalized parts; the
	// current part adds EXTINF durations, so live offsets can differ slightly from playback.
	// The published offset and exactness flag must be read and written under the same lock.
	completedMediaDurationSeconds float64
	mediaOffsetMu                 sync.RWMutex
	mediaOffsetSeconds            float64
	mediaOffsetExact              bool

	cleanupScratch bool
}

func (d *download) setProgress(snap Progress) {
	d.progressMu.Lock()
	d.latestProgress = snap
	d.progressMu.Unlock()
}

func (d *download) progressSnapshot() Progress {
	d.progressMu.RLock()
	defer d.progressMu.RUnlock()
	return d.latestProgress
}

func (d *download) setMediaOffset(seconds float64, exact bool) {
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return
	}
	d.mediaOffsetMu.Lock()
	d.mediaOffsetSeconds = seconds
	d.mediaOffsetExact = exact
	d.mediaOffsetMu.Unlock()
}

// refreshMediaOffset excludes lost media from the playback position.
// Unresolved authentication gaps make the offset provisional because a refetch can move it.
func (d *download) refreshMediaOffset() {
	if d.resume == nil {
		d.setMediaOffset(d.completedMediaDurationSeconds, true)
		return
	}
	d.setMediaOffset(
		d.completedMediaDurationSeconds+d.resume.PartDurationSeconds,
		len(d.resume.AuthGapSeqs()) == 0,
	)
}

// MediaOffsetSeconds returns the current playback position when no refetch can move it.
func (d *download) MediaOffsetSeconds() (float64, bool) {
	d.mediaOffsetMu.RLock()
	defer d.mediaOffsetMu.RUnlock()
	if !d.mediaOffsetExact {
		return 0, false
	}
	return d.mediaOffsetSeconds, true
}

// ResolveMediaOffsetSeconds returns an exact current playback position when available.
func (s *Service) ResolveMediaOffsetSeconds(_ context.Context, broadcasterID string, videoID int64) (float64, bool) {
	if videoID == 0 {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.active {
		if d.videoID == videoID && (broadcasterID == "" || d.broadcasterID == broadcasterID) {
			return d.MediaOffsetSeconds()
		}
	}
	return 0, false
}

func (s *Service) notifyActiveChanged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.activeSubs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// ChannelUpdateSubscriber manages channel.update subscriptions while a recording captures metadata.
type ChannelUpdateSubscriber interface {
	SubscribeChannelUpdate(ctx context.Context, broadcasterID string) error
	UnsubscribeChannelUpdate(ctx context.Context, broadcasterID, reason string) error
}

type titleWatcher interface {
	Watch(ctx context.Context, broadcasterID string, videoID int64, initial streammeta.WatchInitial)
}

// NewService constructs a recording service.
// Nil hydrator, metaWatcher, or channelSubs disables the corresponding metadata integration.
func NewService(cfg *config.Config, repo repository.Repository, store *mediastore.Store, hydrator *streammeta.Hydrator, metaWatcher *streammeta.MetadataWatcher, channelSubs ChannelUpdateSubscriber, log *slog.Logger) *Service {
	domainLog := log.With("domain", "downloader")

	tw := twitch.New(twitch.Config{}, domainLog)

	// The shared host cap must cover every concurrent live and archive recording.
	aggregateHostCap := segmentHostConnectionCap(cfg.App.Download)
	segTransport := &http.Transport{
		MaxConnsPerHost:       aggregateHostCap,
		MaxIdleConnsPerHost:   aggregateHostCap,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableCompression:    true,
	}
	segClient := &http.Client{Transport: segTransport}

	fetcher := hls.NewFetcher(segClient, hls.FetcherConfig{
		TransportAttempts:   cfg.App.Download.NetworkAttempts,
		ServerErrorAttempts: cfg.App.Download.ServerErrorAttempts,
		CDNLagAttempts:      cfg.App.Download.CDNLagAttempts,
		ClassifyAuth:        classifyTwitchAuth,
	}, domainLog)

	s := &Service{
		manual: make(map[string]*manualRun), now: time.Now,
		cfg:         cfg,
		repo:        repo,
		storage:     store,
		log:         domainLog,
		twitch:      tw,
		fetcher:     fetcher,
		remuxer:     &remux.Remuxer{Log: domainLog},
		probe:       &probe.Probe{Log: domainLog},
		thumb:       &thumbnail.Generator{Log: domainLog},
		snapshots:   thumbnail.NewSnapshotter(thumbnail.SnapshotterConfig{Log: domainLog}),
		waveforms:   waveform.FFmpegGenerator{},
		hydrator:    hydrator,
		channelSubs: channelSubs,
		active:      make(map[string]*download),
		activeSubs:  make(map[int]chan struct{}),
	}
	s.work = background.New(map[string]int{"live": s.MaxConcurrent(), "archive": s.ArchiveMaxConcurrent()})
	// A typed nil stored in titleWatcher would pass the nil check and panic on Watch.
	if metaWatcher != nil {
		s.metaWatcher = metaWatcher
	}
	if hydrator != nil {
		s.observe = hydrator.CurrentStream
	}

	return s
}

// PlaybackCredentials supplies the Twitch website session used to acquire playback tokens.
type PlaybackCredentials interface {
	Token(context.Context) (string, error)
	RecheckRejected(context.Context, string) error
}

// SetPlaybackCredentials must be called before Resume and before any job is
// accepted.
func (s *Service) SetPlaybackCredentials(credentials PlaybackCredentials) {
	s.playbackCredentials = credentials
}

// SetEventBus enables committed video notifications and webhook delivery wake-ups.
// Call it before accepting or resuming recordings; nil disables publishing.
func (s *Service) SetEventBus(bus *eventbus.Buses) {
	s.bus = bus
}

// SetPosterStore shares archive poster ownership with background backfill.
// Call it before accepting or resuming recordings.
func (s *Service) SetPosterStore(posters *archiveposter.Store) {
	s.posters = posters
}

func (s *Service) storageReady() error {
	if err := s.storage.Ready(); err != nil {
		return fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
	}
	return nil
}

// publishRecordingTerminal sends a lossy wake-up hint; delivery relies on the committed outbox.
func (s *Service) publishRecordingTerminal(videoID int64, kind eventbus.RecordingTerminalKind) {
	if s.bus == nil || s.bus.RecordingTerminal == nil {
		return
	}
	s.bus.RecordingTerminal.Publish(eventbus.RecordingTerminalEvent{
		VideoID: videoID,
		Kind:    kind,
	})
}

func (s *Service) recordingWebhookDelivery(videoID int64, event string) *repository.RecordingWebhookDeliveryInput {
	return recordingwebhook.NewTerminalDeliveryInput(event, videoID, time.Now().UTC())
}

// sweepOrphanedTempsExcept removes job directories absent from protected.
// The scratch directory must belong exclusively to this service.
func (s *Service) sweepOrphanedTempsExcept(protected map[string]bool) {
	scratch := s.cfg.Env.ScratchDir
	entries, err := os.ReadDir(scratch)
	if err != nil {
		return
	}
	var swept, kept int
	for _, e := range entries {
		if protected[e.Name()] {
			kept++
			continue
		}
		p := filepath.Join(scratch, e.Name())
		if err := os.RemoveAll(p); err != nil {
			s.log.Warn("failed to remove scratch leftover", "path", p, "error", err)
			continue
		}
		swept++
	}
	if swept > 0 || kept > 0 {
		s.log.Info("scratch sweep complete", "swept", swept, "preserved_for_resume", kept)
	}
}

// MaxConcurrent returns the live recording capacity, defaulting to two.
func (s *Service) MaxConcurrent() int {
	if s.cfg.App.Download.MaxConcurrent <= 0 {
		return 2
	}
	return s.cfg.App.Download.MaxConcurrent
}

// Start atomically admits a recording and returns its job ID.
// ErrBusy means the broadcaster already has active or recoverable work.
func (s *Service) Start(ctx context.Context, p Params) (string, error) {
	if s.shuttingDown.Load() {
		return "", ErrShuttingDown
	}
	if err := s.storageReady(); err != nil {
		return "", err
	}
	s.mu.Lock()
	if s.shuttingDown.Load() {
		s.mu.Unlock()
		return "", ErrShuttingDown
	}
	for _, existing := range s.active {
		if !existing.vod && existing.broadcasterID == p.BroadcasterID {
			s.mu.Unlock()
			return "", ErrBusy
		}
	}
	maxConcurrent := s.MaxConcurrent()
	if s.activeLiveCountLocked() >= maxConcurrent {
		s.mu.Unlock()
		return "", fmt.Errorf("downloader: at max concurrent downloads (%d): %w", maxConcurrent, ErrAtCapacity)
	}

	// Recoverable jobs from an earlier process are absent from the active map.
	switch existing, err := s.repo.GetActiveLiveJobByBroadcaster(ctx, p.BroadcasterID); {
	case err == nil && existing != nil:
		s.mu.Unlock()
		return "", ErrBusy
	case err != nil && !errors.Is(err, repository.ErrNotFound):
		s.mu.Unlock()
		return "", fmt.Errorf("check active job: %w", err)
	}

	jobID := uuid.NewString()
	filename := buildFilename(p.BroadcasterLogin, jobID)

	d := &download{
		jobID:         jobID,
		executionID:   uuid.NewString(),
		broadcasterID: p.BroadcasterID,
		attempt:       1,
		progressCh:    make(chan Progress, 16),
		startedAt:     time.Now(),
		resume:        NewResumeState(),
	}
	d.resume.MaxHeight = p.MaxHeight
	reservation, reserveErr := s.work.Reserve("live", jobID)
	if reserveErr != nil {
		s.mu.Unlock()
		return "", ErrAtCapacity
	}
	d.reservation = reservation
	runCtx := reservation.Context()
	cancel := func() { s.work.Cancel(jobID, context.Canceled) }
	d.cancel = cancel
	d.runCtx = runCtx
	s.active[jobID] = d
	s.mu.Unlock()

	cleanupReserved := true
	defer func() {
		if !cleanupReserved {
			return
		}
		cancel()
		s.mu.Lock()
		delete(s.active, jobID)
		s.mu.Unlock()
		d.reservation.Release()
	}()

	checkpoint, err := d.resume.MarshalJSON()
	if err != nil {
		return "", err
	}
	input := &repository.VideoInput{
		JobID:                     jobID,
		Filename:                  filename,
		DisplayName:               p.DisplayName,
		Title:                     p.Title,
		Status:                    repository.VideoStatusPending,
		Quality:                   p.Quality,
		BroadcasterID:             p.BroadcasterID,
		StreamID:                  p.StreamID,
		StreamStartedAt:           p.StreamStartedAt,
		ViewerCount:               p.ViewerCount,
		Language:                  p.Language,
		RecordingType:             p.RecordingType,
		ForceH264:                 p.ForceH264,
		TriggerScheduleID:         p.TriggerScheduleID,
		RetentionSourceScheduleID: p.RetentionSourceScheduleID,
		RetentionWindowHours:      p.RetentionWindowHours,
	}
	if p.TriggerScheduleID == nil && s.cfg.App.Download.StreamerRestartWaitSeconds > 0 {
		input.IntentID = jobID
		input.RestartWaitSeconds = int64(s.cfg.App.Download.StreamerRestartWaitSeconds)
		input.IntentParams, err = json.Marshal(p)
		if err != nil {
			return "", err
		}
	}

	vid, err := repository.CreateAttempt(ctx, s.repo, input, checkpoint)
	if errors.Is(err, repository.ErrCommitUncertain) {
		err = s.persist(runCtx, "admission", func(writeCtx context.Context) error {
			var e error
			vid, e = repository.CreateAttempt(writeCtx, s.repo, input, checkpoint)
			return e
		})
	}

	if err != nil {
		s.mu.Lock()
		delete(s.active, jobID)
		s.mu.Unlock()
		return "", fmt.Errorf("create recording attempt: %w", err)
	}
	s.bus.NotifyVideoChange()

	s.mu.Lock()
	d.videoID = vid.ID
	s.mu.Unlock()

	s.linkInitialMetadata(ctx, vid.ID, p)

	if input.IntentID != "" {
		manual := s.registerManual(input.IntentID, p, d.reservation)
		s.mu.Lock()
		d.manual = manual
		s.mu.Unlock()
	}

	cleanupReserved = false
	s.notifyActiveChanged()
	go s.run(runCtx, d, p, filename)
	return jobID, nil
}

// linkInitialMetadata links opening observations before execution claims.
// The repository rejects late hydration that would reopen an active capture.
func (s *Service) linkInitialMetadata(ctx context.Context, videoID int64, p Params) {
	if s.hydrator == nil {
		return
	}
	if err := s.hydrator.LinkInitialVideoMetadata(ctx, videoID, streammeta.ChannelUpdateMeta{
		Title: p.Title, CategoryID: p.CategoryID, CategoryName: p.CategoryName,
	}); err != nil {
		s.log.Warn("link initial video metadata", "video_id", videoID, "error", err)
	}
}

// Cancel persists the stop before signaling workers. For a manual continuation,
// stopping an older or finalized member also stops the durable intent.
func (s *Service) Cancel(jobID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var intentID string
	if s.repo != nil {
		intent, err := s.repo.GetRecordingIntentByJob(ctx, jobID)
		if err == nil {
			intentID = intent.ID
			if err := s.repo.RequestRecordingIntentStop(ctx, intentID); err != nil {
				return fmt.Errorf("persist manual stop: %w", err)
			}
		} else if !errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("find manual recording intent: %w", err)
		} else if err := repository.RequestAttemptStop(ctx, s.repo, jobID); err != nil {
			return fmt.Errorf("persist recording stop: %w", err)
		}
	}
	s.mu.Lock()
	d := s.active[jobID]
	var cancelAttempt context.CancelFunc
	if d != nil {
		cancelAttempt = d.cancel
		d.userCancelled = true
		if d.manual != nil {
			intentID = d.manual.id
		}
	}
	if intentID != "" {
		for _, child := range s.active {
			if child.manual != nil && child.manual.id == intentID {
				child.userCancelled = true
			}
		}
	}
	s.mu.Unlock()
	if intentID != "" {
		s.work.Cancel(intentID, ErrCancelled)
	} else if cancelAttempt != nil {
		cancelAttempt()
	}
	if intentID == "" && s.repo != nil {
		job, err := s.repo.GetJob(ctx, jobID)
		if err == nil && job.StopRequested && (job.Status == repository.JobStatusPending || job.Status == repository.JobStatusRunning) {
			if err := s.settleUnownedAttempt(ctx, job, ErrCancelled, true); err != nil {
				// Stop is already durable; bounded discovery retries settlement.
				s.log.Warn("stopped recording settlement deferred", "job_id", jobID, "error", err)
			}
		}
	}
	return nil
}

// Subscribe returns a running job's progress channel, or nil when no job owns it.
func (s *Service) Subscribe(jobID string) <-chan Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.active[jobID]; ok {
		return d.progressCh
	}
	return nil
}

// SubscribeActive signals changes to ListActiveProgress snapshots until ctx is cancelled.
// Notifications are coalesced; callers must read a fresh snapshot after each signal.
func (s *Service) SubscribeActive(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)

	s.mu.Lock()
	id := s.nextActiveSubID
	s.nextActiveSubID++
	s.activeSubs[id] = ch
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.activeSubs, id)
		close(ch)
		s.mu.Unlock()
	}()

	return ch
}

// ListActiveProgress returns current progress snapshots, oldest recording first.
func (s *Service) ListActiveProgress() []Progress {
	s.mu.Lock()
	active := make([]*download, 0, len(s.active))
	for _, d := range s.active {
		active = append(active, d)
	}
	s.mu.Unlock()

	sort.Slice(active, func(i, j int) bool {
		return active[i].startedAt.Before(active[j].startedAt)
	})

	out := make([]Progress, 0, len(active))
	for _, d := range active {
		snap := d.progressSnapshot()
		if snap.JobID == "" {
			snap = Progress{
				JobID:         d.jobID,
				PartIndex:     1,
				SegmentsTotal: -1,
			}
		}
		snap.MediaOffsetSeconds = nil
		if seconds, ok := d.MediaOffsetSeconds(); ok {
			snap.MediaOffsetSeconds = &seconds
		}
		out = append(out, snap)
	}
	return out
}

// Shutdown cancels owned workers and waits up to 30 seconds for their settlement.
// Unfinished recordings remain recoverable; a concurrent durable user stop still wins.
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.shuttingDown.Store(true)
	if s.retryCancel != nil {
		s.retryCancel()
	}
	for _, d := range s.active {
		if d.cancel != nil {
			d.cancel()
		}
	}
	s.mu.Unlock()

	if s.work != nil {
		s.work.Stop()
	}
	done := make(chan struct{})
	go func() {
		if s.work != nil {
			_ = s.work.Wait(context.Background())
		}
		s.discoveryWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.log.Info("downloader shutdown: all owned workers stopped")
	case <-time.After(30 * time.Second):
		s.log.Warn("downloader shutdown: 30s timeout reached; some jobs still in flight")
	}
}

// PrepareScratch removes startup leftovers while preserving resumable jobs.
// Bootstrap must call this before any recording, recovery watcher or playback
// build can start. Runtime recovery must never sweep a live scratch directory:
// jobs created after the database snapshot are absent from the protected set.
func (s *Service) PrepareScratch(ctx context.Context) error {
	protected := make(map[string]bool)
	for after := ""; ; {
		jobs, err := s.repo.ListRecoveryJobs(ctx, after, recoveryPageSize)
		if err != nil {
			return fmt.Errorf("list jobs for startup scratch cleanup: %w", err)
		}
		for _, job := range jobs {
			protected[job.ID] = true
			after = job.ID
		}
		if len(jobs) < recoveryPageSize {
			break
		}
	}

	s.sweepOrphanedTempsExcept(protected)
	return nil
}

// Resume discovers unfinished recordings after startup or storage recovery.
// Configure playback credentials and storage first, and call PrepareScratch once at startup.
// Repeated calls skip attempts already owned by this service.
func (s *Service) Resume(ctx context.Context) error {
	s.startArchiveRetryLoop()
	if err := s.resumeRunning(ctx); err != nil {
		return err
	}
	s.PumpArchiveQueue(ctx)
	return nil
}

func (s *Service) resumeRunning(ctx context.Context) error {
	if s.shuttingDown.Load() {
		return ErrShuttingDown
	}
	var failures []error
	if err := s.settleStoppedJobs(ctx); err != nil {
		failures = append(failures, err)
	}
	if err := s.resumeManualIntents(ctx); err != nil {
		failures = append(failures, err)
	}
	for after := ""; ; {
		jobs, err := s.repo.ListRecoveryJobs(ctx, after, recoveryPageSize)
		if err != nil {
			return fmt.Errorf("list recovery jobs: %w", err)
		}

		for i := range jobs {
			job := jobs[i]
			after = job.ID
			if _, err := s.repo.GetRecordingIntentByJob(ctx, job.ID); err == nil {
				continue
			} else if !errors.Is(err, repository.ErrNotFound) {
				failures = append(failures, err)
				continue
			}

			if err := s.restartJob(ctx, &job); err != nil {
				if errors.Is(err, errObsoleteJob) {
					continue
				}
				if errors.Is(err, ErrShuttingDown) || ctx.Err() != nil {
					return err
				}
				if !errors.Is(err, errInvalidResume) && !errors.Is(err, ErrAtCapacity) && !errors.Is(err, ErrStorageUnavailable) {
					failures = append(failures, err)
					continue
				}
				if errors.Is(err, ErrAtCapacity) {
					continue
				}
				if errors.Is(err, ErrStorageUnavailable) {
					// Its media may be on the missing volume; storage recovery will retry it.
					s.log.Warn("resume deferred until storage is attached", "job_id", job.ID, "error", err)
					continue
				}
				if err := s.settleUnownedAttempt(ctx, &job, err, false); err != nil {
					failures = append(failures, fmt.Errorf("persist failed resume %s: %w", job.ID, err))
				}
			}
		}
		if len(jobs) < recoveryPageSize {
			break
		}
	}
	return errors.Join(failures...)
}

// recoveryPageSize bounds discovery reads; checkpoints are still persisted individually.
const recoveryPageSize = 100

var errInvalidResume = errors.New("invalid recording checkpoint")
var errObsoleteJob = errors.New("job no longer owns an active recording")

func (s *Service) restartJob(ctx context.Context, job *repository.Job) error {
	s.mu.Lock()
	if _, exists := s.active[job.ID]; exists {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if job.StopRequested {
		return s.settleUnownedAttempt(ctx, job, ErrCancelled, true)
	}
	if err := s.storageReady(); err != nil {
		return err
	}

	d, p, filename, err := s.reconstructAttempt(ctx, job)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.runCtx = runCtx

	// Recoveries reserve the same domain capacity as new work. Registration
	// and the shutdown check are serialized before any writer can start.
	s.mu.Lock()
	if s.shuttingDown.Load() {
		s.mu.Unlock()
		cancel()
		return ErrShuttingDown
	}
	if _, exists := s.active[job.ID]; exists {
		s.mu.Unlock()
		cancel()
		return nil
	}
	maxConcurrent := s.MaxConcurrent()
	if !d.vod && s.activeLiveCountLocked() >= maxConcurrent {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("at max concurrent downloads (%d); cannot resume: %w", maxConcurrent, ErrAtCapacity)
	}
	kind := "live"
	if d.vod {
		kind = "archive"
	}
	reservation, err := s.work.Reserve(kind, job.ID)
	if err != nil {
		s.mu.Unlock()
		cancel()
		return ErrAtCapacity
	}
	cancel() // replace the setup context with the registered lifetime
	d.reservation = reservation
	runCtx = reservation.Context()
	cancel = func() { s.work.Cancel(job.ID, context.Canceled) }
	d.cancel = cancel
	d.runCtx = runCtx
	s.active[job.ID] = d
	s.mu.Unlock()

	cleanupReserved := true
	defer func() {
		if !cleanupReserved {
			return
		}
		cancel()
		close(d.progressCh)
		s.mu.Lock()
		if s.active[job.ID] == d {
			delete(s.active, job.ID)
		}
		s.mu.Unlock()
		d.reservation.Release()
		s.notifyActiveChanged()
	}()

	if err := s.storageReady(); err != nil {
		return err
	}

	if s.shuttingDown.Load() {
		return ErrShuttingDown
	}

	cleanupReserved = false
	s.notifyActiveChanged()
	go s.run(runCtx, d, p, filename)
	return nil
}

// ErrBusy reports active or recoverable work for the broadcaster; call Cancel
// before replacing an active download.
// ErrAtCapacity reports exhausted live recording capacity.
// ErrCancelled identifies explicit user cancellation.
var (
	ErrBusy         = errors.New("downloader: broadcaster already has an active download")
	ErrShuttingDown = errors.New("downloader: shutting down")
	ErrAtCapacity   = errors.New("downloader: at maximum concurrent downloads")
	// ErrStorageUnavailable means storage is not attached: no recording may start
	// or resume until the operator fixes or adopts it.
	ErrStorageUnavailable = errors.New("downloader: storage is not attached")
	ErrCancelled          = errors.New("downloader: cancelled by user")

	// ErrVariantChanged requires a new part because codec, quality, and frame-rate changes
	// cannot share a copy-remuxed output.
	ErrVariantChanged = errors.New("downloader: selected variant changed mid-run")

	// ErrRestartGapExceeded requires a new part when recovery loses too much broadcast time.
	ErrRestartGapExceeded = errors.New("downloader: resume gap exceeds MaxRestartGapSeconds; forcing part split")

	// ErrPartThresholdExceeded splits at a committed segment boundary without losing media.
	ErrPartThresholdExceeded = errors.New("downloader: part exceeded size/duration ceiling; forcing part split")
)

// startTitleTracking starts the configured metadata observer and returns subscription cleanup.
// The caller must join registered pollers before closing metadata spans.
func (s *Service) startTitleTracking(
	ctx context.Context,
	p Params,
	claim repository.AttemptClaim,
	log *slog.Logger,
	registerPoller func(context.CancelFunc),
	mediaOffset streammeta.MediaOffsetProvider,
) func() {
	noop := func() {}

	if s.cfg.ServerMode.TracksTitlesViaWebhook() && s.channelSubs != nil {
		if err := s.channelSubs.SubscribeChannelUpdate(ctx, p.BroadcasterID); err != nil {
			log.Warn("channel.update subscribe failed; recording keeps only its at-start title",
				"broadcaster_id", p.BroadcasterID, "error", err)
			return noop
		}
		// A live context lets unsubscribe finish after cancellation without consuming
		// Shutdown's full budget; ReconcileChannelUpdateSubs removes subscriptions
		// left by failed DELETEs.
		return func() {
			unsubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			if err := s.channelSubs.UnsubscribeChannelUpdate(unsubCtx, p.BroadcasterID, "recording ended"); err != nil {
				log.Warn("channel.update unsubscribe failed; orphan sub will be swept on next boot",
					"broadcaster_id", p.BroadcasterID, "error", err)
			}
		}
	}

	if s.cfg.ServerMode.TracksTitlesViaPoll() && s.metaWatcher != nil {
		titleCtx, cancelTitle := context.WithCancel(ctx)
		titleScope := background.NewScope(titleCtx)
		registerPoller(func() {
			cancelTitle()
			if err := titleScope.Join(); err != nil {
				log.Warn("title watcher failed", "error", err)
			}
		})
		initial := streammeta.WatchInitial{
			Claim:       claim,
			Title:       p.Title,
			CategoryID:  p.CategoryID,
			MediaOffset: mediaOffset,
		}
		_ = titleScope.Go("titles", false, func(childCtx context.Context) error {
			s.metaWatcher.Watch(childCtx, p.BroadcasterID, claim.VideoID, initial)
			return nil
		})
	}

	return noop
}

func (s *Service) run(_ context.Context, d *download, p Params, filename string) {
	if d.manual != nil {
		s.runManual(d.manual, d, p, filename)
		return
	}
	_ = d.reservation.Run(func(ctx context.Context) error { s.runAttempt(ctx, d, p, filename); return nil }, func(err error) {
		defer func() {
			close(d.progressCh)
			s.mu.Lock()
			delete(s.active, d.jobID)
			s.mu.Unlock()
			s.notifyActiveChanged()
		}()
		defer s.releaseAttemptScratch(d)
		if err != nil {
			s.failDownload(context.Background(), d, s.log.With("job_id", d.jobID), err)
		}
	})
	s.pumpAfterJobEnd()
}

func (s *Service) persistAudioWaveform(ctx context.Context, d *download, filename, recordingType string, totalDuration float64, parts []partResult) error {
	if len(parts) == 0 {
		return nil
	}
	partInputs := make([]waveform.PartInput, 0, len(parts))
	localFiles := make(map[string]string)
	for _, part := range parts {
		if part.filename == "" {
			continue
		}
		partInputs = append(partInputs, waveform.PartInput{
			Filename:        part.filename,
			DurationSeconds: part.durationSeconds,
			SizeBytes:       part.sizeBytes,
		})
		if part.localPath != "" {
			localFiles[part.filename] = part.localPath
		}
	}
	if len(partInputs) == 0 {
		return nil
	}
	var videoDuration *float64
	if totalDuration > 0 {
		videoDuration = &totalDuration
	}
	plan, ok := waveform.BuildPlan(d.videoID, recordingType, videoDuration, partInputs)
	if !ok {
		return nil
	}
	if err := s.waitForStorage(ctx); err != nil {
		return err
	}
	resp, err := waveform.Generate(ctx, s.waveforms, waveform.InputResolver{
		Storage:    s.storage,
		LocalFiles: localFiles,
	}, plan)
	if err != nil {
		return err
	}
	return s.writeToStorage(ctx, func() error {
		owned, err := s.storage.ForAttempt(ctx, d.claim())
		if err != nil {
			return err
		}
		defer owned.Close()
		return waveform.SaveArtifact(ctx, owned, filename, plan.Fingerprint, resp)
	})
}

func (s *Service) continueAfterPendingSplit(dbCtx context.Context, d *download, emitter *progressEmitter, log *slog.Logger) (bool, error) {
	shouldContinue, err := d.resume.ShouldOpenNextPart(
		MaxDiscontinuityPartsPerVideo,
		thresholdPartCap(s.cfg.App.Download.MaxPartCount),
	)
	if err != nil {
		return false, err
	}
	if !shouldContinue {
		return false, nil
	}

	// Threshold splits retain the variant and sequence space; discontinuities must reanchor.
	if d.resume.PendingThresholdSplit {
		d.resume.ContinuePart()
	} else {
		d.resume.BeginNewPart()
	}
	d.videoPartID = 0
	emitter.setPart(int(d.resume.CurrentPartIndex))
	s.checkpointResume(dbCtx, d, log)
	return true, nil
}

func (s *Service) reanchorCurrentPartAfterEmptySplit(dbCtx context.Context, d *download, emitter *progressEmitter, log *slog.Logger) (bool, error) {
	shouldContinue, err := d.resume.ShouldOpenNextPart(
		MaxDiscontinuityPartsPerVideo,
		thresholdPartCap(s.cfg.App.Download.MaxPartCount),
	)
	if err != nil {
		return false, err
	}
	if !shouldContinue {
		return false, nil
	}

	d.resume.ReanchorCurrentPartAfterEmptySplit()
	d.videoPartID = 0
	emitter.setPart(int(d.resume.CurrentPartIndex))
	s.checkpointResume(dbCtx, d, log)
	return true, nil
}

func isSplitSignal(err error) bool {
	return errors.Is(err, hls.ErrPlaylistGone) ||
		errors.Is(err, ErrVariantChanged) ||
		errors.Is(err, ErrRestartGapExceeded) ||
		errors.Is(err, ErrPartThresholdExceeded)
}

// mapForcedSplitErr preserves parent cancellation over forced part splits.
// A sealed boundary owns all earlier media, so errors beyond it belong to the next part.
func mapForcedSplitErr(ctx context.Context, err error, fired, boundarySealed bool, sentinel error, msg string) error {
	if !fired || ctx.Err() != nil {
		return err
	}
	if boundarySealed || err == nil || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %s", sentinel, msg)
	}
	return err
}

// fpsEqual compares declared frame rates exactly because a changed rate requires a new part.
func fpsEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func fpsDisplay(f *float64) any {
	if f == nil {
		return "<none>"
	}
	return *f
}

// partEndMediaSeq excludes in-flight results beyond a sealed threshold boundary.
func partEndMediaSeq(hlsResult *hls.JobResult, resume *ResumeState) int64 {
	if resume.PendingThresholdSplit && resume.PendingSplitBoundarySet {
		return resume.PendingSplitBoundaryMediaSeq
	}
	last := int64(0)
	if hlsResult != nil {
		last = hlsResult.LastMediaSeq
	}
	return max(resume.AccountedFrontierMediaSeq, last)
}

func synthesizeHLSResultFromResume(resume *ResumeState, kind hls.SegmentKind) *hls.JobResult {
	last := resume.AccountedFrontierMediaSeq
	if resume.PendingThresholdSplit && resume.PendingSplitBoundarySet {
		last = resume.PendingSplitBoundaryMediaSeq
	}
	done := int64(0)
	if resume.PartStarted && last >= resume.PartStartMediaSequence {
		done = last - resume.PartStartMediaSequence + 1 - gapSeqCount(resume.Gaps)
	}
	if done < 0 {
		done = 0
	}
	endList := resume.EndListSeen
	if resume.PendingThresholdSplit && resume.PendingSplitBoundarySet && !resume.PendingSplitEndListAtBoundary {
		endList = false
	}
	return &hls.JobResult{
		Kind:         kind,
		LastMediaSeq: last,
		SegmentsDone: done,
		SegmentsGaps: gapSeqCount(resume.Gaps),
		EndList:      endList,
	}
}

func shouldSkipSegmentFetch(resume *ResumeState) bool {
	return resume.Stage.AtOrAfter(StagePrepareInput) || resume.PendingSplit || resume.CaptureError != ""
}

func gapSeqCount(gaps []Gap) int64 {
	var n int64
	for _, g := range gaps {
		end := max(g.EndMediaSeq, g.MediaSeq)
		if end >= g.MediaSeq {
			n += end - g.MediaSeq + 1
		}
	}
	return n
}

func segmentKindForResume(segmentsDir string, resume *ResumeState) hls.SegmentKind {
	switch hls.SegmentKind(resume.SegmentFormat) {
	case hls.SegmentKindTS, hls.SegmentKindFMP4:
		return hls.SegmentKind(resume.SegmentFormat)
	}
	if hasSegmentExt(segmentsDir, ".m4s") {
		return hls.SegmentKindFMP4
	}
	return hls.SegmentKindTS
}

func hasSegmentExt(dir, ext string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ext) {
			return true
		}
	}
	return false
}

func hasSegmentFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.Mode().IsRegular()
}

func shouldFinalizeEmptyContinuation(priorParts int, hlsResult *hls.JobResult, resume *ResumeState) bool {
	return priorParts > 0 &&
		hlsResult != nil &&
		(hlsResult.EndList || resume.CaptureStoppedAt != nil) &&
		!currentPartHasCommittedMedia(hlsResult, resume)
}

func shouldSkipEmptySplitPart(priorParts int, hlsResult *hls.JobResult, resume *ResumeState) bool {
	// Saved segments count as content even when this attempt fetched nothing.
	return priorParts > 0 &&
		resume.PendingSplit &&
		!currentPartHasCommittedMedia(hlsResult, resume)
}

func shouldAcceptEmptySplitSignal(priorParts int, hlsResult *hls.JobResult, err error) bool {
	return priorParts > 0 &&
		isSplitSignal(err) &&
		!hasCommittedMedia(hlsResult)
}

func hasCommittedMedia(hlsResult *hls.JobResult) bool {
	return hlsResult != nil && hlsResult.SegmentsDone > 0
}

// resumePartHasCommittedMedia reports whether the checkpoint owns a non-gap segment.
// An attempt that fetched nothing may still have saved media from before recovery.
func resumePartHasCommittedMedia(resume *ResumeState) bool {
	if len(resume.CompletedAboveFrontier) > 0 {
		return true
	}
	if !resume.PartStarted || resume.AccountedFrontierMediaSeq < resume.PartStartMediaSequence {
		return false
	}
	span := resume.AccountedFrontierMediaSeq - resume.PartStartMediaSequence + 1
	return span > gapSeqCount(resume.Gaps)
}

// currentPartHasCommittedMedia includes durable media so empty recovery fetches cannot discard it.
func currentPartHasCommittedMedia(hlsResult *hls.JobResult, resume *ResumeState) bool {
	return hasCommittedMedia(hlsResult) || resumePartHasCommittedMedia(resume)
}

func captureHadWindowRoll(resume *ResumeState) bool {
	before := resume.HadWindowRoll
	if !resume.HadWindowRoll {
		for _, g := range resume.Gaps {
			if g.Reason == GapReasonRestartWindowRolled {
				resume.HadWindowRoll = true
				break
			}
		}
	}
	return resume.HadWindowRoll != before
}

func thresholdSplitEndListAtBoundary(resume *ResumeState, hlsResult *hls.JobResult) bool {
	return hlsResult != nil &&
		hlsResult.EndList &&
		resume.PendingThresholdSplit &&
		resume.PendingSplitBoundarySet &&
		hlsResult.LastMediaSeq <= resume.PendingSplitBoundaryMediaSeq
}

func shouldPersistEndListSeen(resume *ResumeState, hlsResult *hls.JobResult) bool {
	if hlsResult == nil || !hlsResult.EndList {
		return false
	}
	if resume.PendingThresholdSplit &&
		resume.PendingSplitBoundarySet &&
		hlsResult.LastMediaSeq > resume.PendingSplitBoundaryMediaSeq {
		return false
	}
	return true
}

func pendingSplitEndedAtBoundary(resume *ResumeState, hlsResult *hls.JobResult) bool {
	if !resume.PendingSplit || !resume.EndListSeen || hlsResult == nil || !hlsResult.EndList {
		return false
	}
	if !resume.PendingThresholdSplit || !resume.PendingSplitBoundarySet {
		return true
	}
	if !resume.PendingSplitEndListAtBoundary {
		return false
	}
	return hlsResult.LastMediaSeq <= resume.PendingSplitBoundaryMediaSeq
}

func pruneSegmentsAfterBoundary(dir string, kind hls.SegmentKind, boundary int64, workspace *mediastore.Workspace) error {
	remove := os.Remove
	if workspace != nil {
		remove = workspace.Remove
	}
	ext := ".ts"
	if kind == hls.SegmentKindFMP4 {
		ext = ".m4s"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read segments dir: %w", err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		seqText := strings.TrimSuffix(e.Name(), ext)
		seq, err := strconv.ParseInt(seqText, 10, 64)
		if err != nil {
			continue
		}
		if seq <= boundary {
			continue
		}
		if err := remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove post-boundary segment %s: %w", e.Name(), err)
		}
	}
	return nil
}

func thresholdPartCap(configured int32) int32 {
	if configured <= 0 {
		return DefaultMaxThresholdPartsPerVideo
	}
	return configured
}

// shouldForceSplitOnRestartGap reports whether a lost window exceeds the configured gap.
// A nonpositive threshold disables splitting.
func shouldForceSplitOnRestartGap(from, to int64, targetDuration time.Duration, thresholdSeconds int, resume *ResumeState) bool {
	if thresholdSeconds <= 0 {
		return false
	}
	lost := time.Duration(to-from+1) * targetDuration
	threshold := time.Duration(thresholdSeconds) * time.Second
	return lost > threshold && hasPartContent(nil, resume)
}

// hasPartContent distinguishes an unstarted part from a part anchored at media sequence zero.
func hasPartContent(hlsResult *hls.JobResult, resume *ResumeState) bool {
	if hlsResult != nil && hlsResult.SegmentsDone > 0 {
		return true
	}
	return resume.PartStarted &&
		resume.AccountedFrontierMediaSeq >= resume.PartStartMediaSequence
}

// runPart prepares and publishes one part without settling the whole recording.
func (s *Service) runPart(ctx, dbCtx context.Context, d *download, p Params,
	filename string, segmentsDir string, hlsResult *hls.JobResult,
	emitter *progressEmitter, log *slog.Logger) (*partResult, error) {
	if prepared := d.resume.PreparedPart; prepared != nil {
		return s.publishPreparedPart(ctx, d, prepared, log)
	}

	jobDir := filepath.Dir(filepath.Dir(segmentsDir))
	partIndex := d.resume.CurrentPartIndex
	partFilename := fmt.Sprintf("%s-part%02d", filename, partIndex)

	recordingType := p.RecordingType
	if recordingType == "" {
		recordingType = twitch.RecordingTypeVideo
	}
	kind := kindFromRecordingType(recordingType)
	remuxMode := remux.ModeTS
	if hlsResult.Kind == hls.SegmentKindFMP4 {
		remuxMode = remux.ModeFMP4
	}
	// Save the container format so recovery can prepare input without fetching a playlist.
	d.resume.SegmentFormat = string(hlsResult.Kind)

	// Create the part before preparation so every later stage can recover its database identity.
	if err := s.persist(ctx, "create video part", func(writeCtx context.Context) error {
		return repository.WithAttempt(writeCtx, s.repo, d.claim(), func(tx repository.Repository) error {
			part, err := tx.GetVideoPartByIndex(writeCtx, d.videoID, partIndex)
			if errors.Is(err, repository.ErrNotFound) {
				part, err = tx.CreateVideoPart(writeCtx, &repository.VideoPartInput{
					VideoID: d.videoID, PartIndex: partIndex, Filename: partFilename + kind.OutputExt(),
					Quality: d.resume.SelectedQuality, FPS: d.resume.SelectedFPS,
					Codec: d.resume.SelectedCodec, SegmentFormat: d.resume.SegmentFormat,
					StartMediaSeq: d.resume.PartStartMediaSequence,
				})
			}
			if err != nil {
				return err
			}
			d.videoPartID = part.ID
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("persist video part: %w", err)
	}

	// Preparation is repeatable after a crash between input creation and the next checkpoint.
	s.setResumeStage(dbCtx, d, StagePrepareInput, log)
	emitter.setStage("remux")
	var files remux.FileOperations
	remove, rename := os.Remove, os.Rename
	if d.workspace != nil {
		files = d.workspace
		remove, rename = d.workspace.Remove, d.workspace.Rename
	}
	inputPath, err := remux.PrepareInput(ctx, segmentsDir, remuxMode, files)
	if err != nil {
		return nil, fmt.Errorf("remux prep: %w", err)
	}

	if d.workspace != nil {
		// The remux and an optional healed replacement can coexist with captured input.
		estimate := d.resume.PartBytes*2 + d.resume.PartBytes/64
		if err := d.workspace.ReserveAdditional(ctx, estimate); err != nil {
			d.persistenceErr = err
			return nil, err
		}
	}
	// Remux publishes via rename so an interrupted attempt cannot expose partial output.
	s.setResumeStage(dbCtx, d, StageRemux, log)
	remuxIn := remux.RunInput{
		Files:          files,
		Mode:           remuxMode,
		Kind:           kind,
		InputPath:      inputPath,
		OutputDir:      jobDir,
		OutputBasename: partFilename,
	}
	if err := s.remuxer.Run(ctx, remuxIn); err != nil {
		return nil, fmt.Errorf("remux: %w", err)
	}
	remuxedPath := remuxIn.OutputPath()

	s.setResumeStage(dbCtx, d, StageProbe, log)
	emitter.setStage("metadata")
	probeResult, err := s.probe.Run(ctx, remuxedPath)
	if err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}

	// Keep the existing output if healing or its validation fails.
	if isCorrupt(probeResult, kind) {
		s.setResumeStage(dbCtx, d, StageCorruptionCheck, log)
		log.Info("duration mismatch — running heal pass",
			"part_index", partIndex,
			"format_duration", probeResult.Duration,
			"threshold", remux.CorruptionThreshold)
		healedPath := filepath.Join(jobDir, partFilename+".healed"+kind.OutputExt())
		if err := s.remuxer.Heal(ctx, remuxedPath, healedPath, kind, files); err != nil {
			log.Warn("heal failed; keeping un-healed file", "error", err)
		} else if healedResult, probeErr := s.probe.Run(ctx, healedPath); probeErr != nil {
			log.Warn("re-probe of healed file failed; keeping un-healed", "error", probeErr)
			_ = remove(healedPath)
		} else if isCorrupt(healedResult, kind) {
			log.Warn("heal did not resolve corruption; keeping un-healed")
			_ = remove(healedPath)
		} else {
			if err := rename(healedPath, remuxedPath); err != nil {
				log.Warn("heal-rename failed; keeping un-healed", "error", err)
				_ = remove(healedPath)
			} else {
				probeResult = healedResult
			}
		}
	}

	var thumbRel string
	var stripRel string
	if kind == remux.KindVideo {
		s.setResumeStage(dbCtx, d, StageThumbnail, log)
		emitter.setStage("thumbnail")
		thumbPath := filepath.Join(jobDir, partFilename+".jpg")
		err := s.thumb.Generate(ctx, thumbnail.Input{
			Files:           files,
			VideoPath:       remuxedPath,
			OutputPath:      thumbPath,
			DurationSeconds: probeResult.Duration,
		})
		switch {
		case err == nil:
			thumbRel = storagekeys.Thumbnail(partFilename)
		case errors.Is(err, thumbnail.ErrAllTriesSingleColor):
			log.Info("thumbnail: all frames monochrome; leaving unset")
		default:
			log.Warn("thumbnail generation failed; continuing without thumbnail", "error", err)
		}

		// The player falls back to the thumbnail when no sprite strip is available.
		if probeResult.Duration > 0 {
			stripPath := filepath.Join(jobDir, partFilename+"-strip.jpg")
			if err := s.thumb.GenerateStrip(ctx, thumbnail.StripInput{
				Files:           files,
				VideoPath:       remuxedPath,
				OutputPath:      stripPath,
				DurationSeconds: probeResult.Duration,
			}); err != nil {
				log.Warn("strip generation failed; continuing without strip", "error", err)
			} else {
				stripRel = storagekeys.Strip(partFilename)
			}
		}
	}

	// Persist the exact output before starting publication. Recovery reuses
	// these bytes and probe facts, including after a lost finalization acknowledgement.
	prepared := &PreparedPart{
		Filename: partFilename + kind.OutputExt(), Path: remuxedPath,
		Facts:     repository.VideoPartFinalize{ID: d.videoPartID, DurationSeconds: probeResult.Duration, SizeBytes: probeResult.Size, EndMediaSeq: partEndMediaSeq(hlsResult, d.resume)},
		Thumbnail: thumbRel, ThumbnailPath: filepath.Join(jobDir, partFilename+".jpg"),
		Strip: stripRel, StripPath: filepath.Join(jobDir, partFilename+"-strip.jpg"),
	}
	digest, err := preparedDigest(ctx, remuxedPath)
	if err != nil {
		return nil, err
	}
	prepared.Digest = digest
	d.resume.PreparedPart = prepared
	s.setResumeStage(dbCtx, d, StageStore, log)
	if d.persistenceErr != nil {
		return nil, d.persistenceErr
	}
	return s.publishPreparedPart(ctx, d, prepared, log)
}

// partOutgrewSource reports growth beyond the captured source size when size splitting is enabled.
// Container overhead and fMP4 initialization bytes can make output exceed the source ceiling.
func partOutgrewSource(outputBytes, sourceBytes, maxPartBytes int64) bool {
	return maxPartBytes > 0 && sourceBytes > 0 && outputBytes > sourceBytes
}

// partThresholdAccountant checks split thresholds whenever the durable frontier advances.
// Authentication gaps remain refetchable; restart-gap splits take precedence over size limits.
type partThresholdAccountant struct {
	resume     *ResumeState
	maxBytes   int64
	maxSeconds int
	onSeal     func(boundary int64)
}

func (a *partThresholdAccountant) commit(seq, bytes int64, dur float64) {
	a.sealIfCrossed(a.resume.NoteCommittedSegmentUntilThreshold(seq, bytes, dur, a.maxBytes, a.maxSeconds))
}

func (a *partThresholdAccountant) gap(seq int64, reason GapReason) {
	a.sealIfCrossed(a.resume.NoteGapUntilThreshold(seq, reason, a.maxBytes, a.maxSeconds))
}

// authGap preserves refetchable authentication gaps without sealing a permanent hole.
func (a *partThresholdAccountant) authGap(seq int64) {
	a.resume.NoteGap(seq, GapReasonAuth)
}

// recordRangeGap returns a threshold crossing without sealing it, so restart-gap splits can win.
func (a *partThresholdAccountant) recordRangeGap(from, to int64, reason GapReason) (int64, bool) {
	return a.resume.NoteRangeGapUntilThreshold(from, to, reason, a.maxBytes, a.maxSeconds)
}

func (a *partThresholdAccountant) sealIfCrossed(boundary int64, crossed bool) bool {
	if !crossed || a.resume.PendingSplit || !hasPartContent(nil, a.resume) {
		return false
	}
	a.resume.PendingSplit = true
	a.resume.PendingThresholdSplit = true
	a.resume.SealThresholdSplitBoundary(boundary)
	a.onSeal(boundary)
	return true
}

// fetchWithAuthRefresh renews expired playback URLs within the authentication retry budget.
// Gap policy uses cumulative per-part counters across attempts, and permanent failures stop
// immediately.
func (s *Service) fetchWithAuthRefresh(ctx, dbCtx context.Context, d *download, emitter *progressEmitter, p Params, segmentsDir string, selectOpts twitch.SelectOptions, log *slog.Logger) (*hls.JobResult, error) {
	maxAuthAttempts := s.cfg.App.Download.AuthRefreshAttempts
	if maxAuthAttempts <= 0 {
		maxAuthAttempts = 2
	}

	agg := &hls.JobResult{}
	var authAttempts int
	unresolvedCanceled := map[int64]bool{}

	// Seed authentication retries from the checkpoint so a crash cannot lose pending refetches.
	refetchSeqs := d.resume.AuthGapSeqs()

	// Authentication refreshes must retain the part's first playlist anchor.
	bootstrapped := d.resume.PartStarted

	var startSeq int64
	if bootstrapped {
		startSeq = d.resume.AccountedFrontierMediaSeq + 1
	}

	// Batch checkpoints to bound database traffic during capture.
	const checkpointEveryEvents = 10
	var eventsSinceCheckpoint int

	for {
		emitter.setStage("auth")
		variant, err := retryPlaybackResolution(ctx, func(attemptCtx context.Context) (twitch.SelectedVariant, error) {
			if err := s.verifyPlaybackIdentity(attemptCtx, p); err != nil {
				return twitch.SelectedVariant{}, err
			}
			variant, err := s.resolveVariantURL(attemptCtx, p, selectOpts)
			if err != nil {
				return variant, err
			}
			// Playback resolves by channel, including on authentication retries.
			// A delayed resolution must not cross a broadcast identity boundary.
			return variant, s.verifyPlaybackIdentity(attemptCtx, p)
		})
		if err != nil {
			if errors.Is(err, errBroadcastLookup) {
				d.persistenceErr = err // Preserve capture until identity can be verified.
			}
			// Retryable resolution failures have exhausted their independent
			// budget. The caller can seal already captured media before failing.
			return agg, err
		}
		// Copy-remuxing requires one quality, codec, and frame rate per part.
		if d.resume.SelectedQuality != "" && d.resume.SelectedQuality != variant.Quality {
			return agg, fmt.Errorf("%w: quality %q → %q",
				ErrVariantChanged, d.resume.SelectedQuality, variant.Quality)
		}
		if d.resume.SelectedCodec != "" && d.resume.SelectedCodec != variant.Codec {
			return agg, fmt.Errorf("%w: codec %q → %q",
				ErrVariantChanged, d.resume.SelectedCodec, variant.Codec)
		}
		if d.resume.SelectedFPS != nil && !fpsEqual(d.resume.SelectedFPS, variant.FPS) {
			return agg, fmt.Errorf("%w: fps %v → %v",
				ErrVariantChanged, *d.resume.SelectedFPS, fpsDisplay(variant.FPS))
		}
		emitter.setStage("playlist")
		emitter.setVariant(variant.Quality, variant.FPS, variant.Codec)
		if err := s.persist(ctx, "selected variant", func(writeCtx context.Context) error {
			return repository.WithAttempt(writeCtx, s.repo, d.claim(), func(tx repository.Repository) error {
				return tx.UpdateVideoSelectedVariant(writeCtx, d.videoID, variant.Quality, variant.FPS)
			})
		}); err != nil {
			return agg, fmt.Errorf("persist selected variant: %w", err)
		}
		// Save the variant for recovery without another master playlist request.
		d.resume.SelectedQuality = variant.Quality
		d.resume.SelectedFPS = variant.FPS
		d.resume.SelectedCodec = variant.Codec

		// The scoped cancellation flags distinguish a forced split from parent shutdown.
		splitCtx, cancelSplit := context.WithCancel(ctx)
		thresholdSeconds := s.cfg.App.Download.MaxRestartGapSeconds
		maxPartBytes := s.cfg.App.Download.MaxPartBytes
		maxPartSeconds := s.cfg.App.Download.MaxPartSeconds
		var thresholdSplitFired bool
		var partThresholdFired bool

		acct := &partThresholdAccountant{
			resume:     d.resume,
			maxBytes:   maxPartBytes,
			maxSeconds: maxPartSeconds,
			onSeal: func(boundary int64) {
				partThresholdFired = true
				log.Info("part reached size/duration ceiling; forcing part boundary",
					"part_index", d.resume.CurrentPartIndex,
					"boundary_media_seq", boundary,
					"part_bytes", d.resume.PartBytes,
					"part_seconds", d.resume.PartDurationSeconds,
					"max_part_bytes", maxPartBytes,
					"max_part_seconds", maxPartSeconds)
				s.checkpointResume(dbCtx, d, log)
				cancelSplit()
			},
		}

		// Check restart-gap splitting before sealing a size threshold crossed by buffered commits.
		recordWindowRollGap := func(from, to int64, logMsg string, split func() bool) {
			boundary, thresholdReached := acct.recordRangeGap(from, to, GapReasonRestartWindowRolled)
			d.refreshMediaOffset()
			log.Warn(logMsg,
				"reason", GapReasonRestartWindowRolled,
				"from", from,
				"to", to,
				"lost_segments", to-from+1)
			if split != nil && split() {
				return
			}
			if acct.sealIfCrossed(boundary, thresholdReached) {
				return
			}
			s.checkpointResume(dbCtx, d, log)
		}

		result, err := runHLSAttempt(splitCtx, emitter, hls.JobConfig{
			Files:              d.workspace,
			MediaPlaylistURL:   variant.URL,
			WorkDir:            segmentsDir,
			Fetcher:            s.fetcher,
			SegmentConcurrency: s.cfg.App.Download.SegmentConcurrency,
			Log:                log,
			StartMediaSeq:      startSeq,
			ClassifyAuth:       classifyTwitchAuth,
			RateLimiter:        d.limiter,
			// Authentication refreshes must retain the part's gap-policy history.
			SeedSegmentsDone: agg.SegmentsDone,
			SeedSegmentsGaps: agg.SegmentsGaps,
			RefetchSeqs:      refetchSeqs,
			GapPolicy: hls.GapPolicy{
				Strict:      s.cfg.App.Download.Strict,
				MaxGapRatio: s.cfg.App.Download.MaxGapRatio,
			},
			OnFirstPoll: func(first hls.PollResult) {
				if d.resume.SegmentFormat == "" {
					d.resume.SegmentFormat = string(first.Kind)
				}
				if bootstrapped {
					s.checkpointResume(dbCtx, d, log)
					return
				}
				bootstrapped = true
				d.resume.StartPart(first.MediaSequenceBase)
				s.checkpointResume(dbCtx, d, log)
			},
			OnWindowRoll: func(from, to int64, targetDuration time.Duration) {
				// A large first-poll resume roll starts a new part before threshold sealing.
				recordWindowRollGap(from, to, "resume gap recorded", func() bool {
					if !shouldForceSplitOnRestartGap(from, to, targetDuration, thresholdSeconds, d.resume) {
						return false
					}
					d.resume.PendingSplit = true
					thresholdSplitFired = true
					log.Info("restart gap exceeds threshold; forcing part boundary",
						"from", from,
						"to", to,
						"lost_seconds", (time.Duration(to-from+1) * targetDuration).Seconds(),
						"threshold_seconds", thresholdSeconds,
						"part_index", d.resume.CurrentPartIndex)
					s.checkpointResume(dbCtx, d, log)
					cancelSplit()
					return true
				})
			},
			OnMidStreamWindowRoll: func(from, to int64) {
				// Mid-stream holes stay inside the current part.
				recordWindowRollGap(from, to, "mid-stream window roll recorded as gap", nil)
			},
			OnEvent: func(ev hls.SegmentEvent) {
				if d.resume.PendingThresholdSplit &&
					d.resume.PendingSplitBoundarySet &&
					ev.MediaSeq > d.resume.PendingSplitBoundaryMediaSeq {
					return
				}
				switch ev.Outcome {
				case hls.OutcomeCommitted:
					// Only contiguous committed media may seal a split; the next part refetches its tail.
					acct.commit(ev.MediaSeq, ev.BytesWritten, ev.DurationSeconds)
				case hls.OutcomeGapAccepted:
					acct.gap(ev.MediaSeq, GapReasonFetchFailure)
				case hls.OutcomeAdSkipped:
					acct.gap(ev.MediaSeq, GapReasonStitchedAd)
				case hls.OutcomeMalformedSkip:
					acct.gap(ev.MediaSeq, GapReasonMalformed)
				case hls.OutcomeAuth:
					acct.authGap(ev.MediaSeq)
				}
				d.refreshMediaOffset()
				eventsSinceCheckpoint++
				if eventsSinceCheckpoint >= checkpointEveryEvents {
					s.checkpointResume(dbCtx, d, log)
					eventsSinceCheckpoint = 0
				}
			},
		})
		cancelSplit()
		err = mapForcedSplitErr(ctx, err, thresholdSplitFired, false, ErrRestartGapExceeded, "forced part split at restart gap")
		err = mapForcedSplitErr(ctx, err, partThresholdFired, d.resume.PendingSplitBoundarySet, ErrPartThresholdExceeded,
			fmt.Sprintf("part %d reached size/duration ceiling", d.resume.CurrentPartIndex))
		// Persist trailing events before another authentication attempt starts.
		s.checkpointResume(dbCtx, d, log)
		eventsSinceCheckpoint = 0

		refetchSeqs = nil

		// Done and gap counters are seeded per part; bytes and ad gaps are per attempt.
		if result != nil {
			foldHLSAttemptResult(agg, result, d.resume, unresolvedCanceled)
			refetchSeqs = refetchSeqsForNextAttempt(result.AuthErrorSeqs, unresolvedCanceled)
			startSeq = agg.LastMediaSeq + 1
		}

		if err == nil {
			return agg, nil
		}
		if !errors.Is(err, hls.ErrPlaylistAuth) {
			return agg, fmt.Errorf("hls run: %w", err)
		}
		authAttempts++
		if authAttempts > maxAuthAttempts {
			return agg, fmt.Errorf("auth refresh budget exhausted after %d attempts: %w", authAttempts, err)
		}
		log.Info("playback URL expired; refreshing",
			"attempt", authAttempts,
			"budget", maxAuthAttempts,
			"resume_from_seq", startSeq)
	}
}

// refetchSeqsForNextAttempt retains cancelled segments below the advanced cursor for retry.
// Without explicit refetches, those holes could prevent the durable frontier from advancing.
func refetchSeqsForNextAttempt(authErrorSeqs []int64, unresolvedCanceled map[int64]bool) []int64 {
	out := append([]int64(nil), authErrorSeqs...)
	for seq := range unresolvedCanceled {
		if !slices.Contains(out, seq) {
			out = append(out, seq)
		}
	}
	slices.Sort(out)
	return out
}

func foldHLSAttemptResult(agg, result *hls.JobResult, resume *ResumeState, unresolvedCanceled map[int64]bool) {
	for _, seq := range result.CanceledSeqs {
		if !resume.ShouldSkip(seq) {
			unresolvedCanceled[seq] = true
		}
	}
	for seq := range unresolvedCanceled {
		if resume.ShouldSkip(seq) {
			delete(unresolvedCanceled, seq)
		}
	}

	agg.SegmentsDone = result.SegmentsDone
	agg.SegmentsGaps = result.SegmentsGaps
	agg.SegmentsAdGaps += result.SegmentsAdGaps
	agg.SegmentsCanceled += result.SegmentsCanceled
	agg.BytesWritten += result.BytesWritten
	if result.Kind != "" {
		agg.Kind = result.Kind
	}
	if result.InitURI != "" {
		agg.InitURI = result.InitURI
	}
	if result.LastMediaSeq > agg.LastMediaSeq {
		agg.LastMediaSeq = result.LastMediaSeq
	}
	// ENDLIST is valid only after all earlier cancelled segments are durably resolved.
	if result.EndList && len(unresolvedCanceled) == 0 {
		agg.EndList = true
	}
}

func (s *Service) resolveVariantURL(ctx context.Context, p Params, opts twitch.SelectOptions) (twitch.SelectedVariant, error) {
	manifest, _, err := s.resolveManifest(ctx, p, opts)
	if err != nil {
		return twitch.SelectedVariant{}, err
	}
	variant, err := twitch.SelectVariant(manifest, opts)
	if err != nil {
		return twitch.SelectedVariant{}, fmt.Errorf("variant selection: %w", err)
	}
	return variant, nil
}

// LiveRenditions lists available video variants; Anonymous reports whether playback used no
// session.
type LiveRenditions struct {
	Anonymous  bool
	Renditions []Rendition
}

// Rendition describes a live video variant; FPS is zero when Twitch omits the frame rate.
type Rendition struct {
	Height int
	FPS    float64
	Codec  string
}

// LiveRenditions lists video variants tallest first, then by codec and frame-rate preference.
// forceH264 changes the playback request because Twitch may return a different manifest.
func (s *Service) LiveRenditions(ctx context.Context, login string, forceH264 bool) (LiveRenditions, error) {
	opts := twitch.SelectOptions{
		RecordingType: twitch.RecordingTypeVideo,
		Quality:       "best",
		EnableAV1:     s.cfg.App.Download.EnableAV1,
		DisableHEVC:   s.cfg.App.Download.DisableHEVC,
		ForceH264:     forceH264,
	}
	manifest, anonymous, err := s.resolveManifest(ctx, Params{BroadcasterLogin: login}, opts)
	if err != nil {
		return LiveRenditions{}, err
	}
	pool := twitch.AcceptableVariants(manifest, opts)
	twitch.SortByPreference(pool)
	out := LiveRenditions{Anonymous: anonymous, Renditions: make([]Rendition, 0, len(pool))}
	for _, v := range pool {
		height, err := strconv.Atoi(v.Quality)
		if err != nil || height <= 0 {
			continue
		}
		out.Renditions = append(out.Renditions, Rendition{Height: height, FPS: v.FPS, Codec: v.Codec})
	}
	return out, nil
}

// resolveManifest returns a master playlist and reports whether playback was anonymous.
// Website credentials are sent only to Twitch's token endpoint; rejected sessions retry
// anonymously.
func (s *Service) resolveManifest(ctx context.Context, p Params, opts twitch.SelectOptions) (*twitch.Manifest, bool, error) {
	var accessToken string
	if s.playbackCredentials != nil {
		var err error
		accessToken, err = s.playbackCredentials.Token(ctx)
		if errors.Is(err, playbackauth.ErrRejected) {
			accessToken = ""
		} else if err != nil {
			return nil, false, fmt.Errorf("resolve Twitch playback connection: %w", err)
		}
	}
	playbackToken := func(accessToken string) (twitch.PlaybackToken, error) {
		if p.isVOD() {
			return s.twitch.VODPlaybackToken(ctx, p.VODID, accessToken)
		}
		return s.twitch.PlaybackToken(ctx, p.BroadcasterLogin, accessToken)
	}
	token, err := playbackToken(accessToken)
	if err != nil {
		var authErr *twitch.AuthError
		if s.playbackCredentials != nil && accessToken != "" && errors.As(err, &authErr) && authErr.Status == http.StatusUnauthorized {
			checkErr := s.playbackCredentials.RecheckRejected(ctx, accessToken)
			if errors.Is(checkErr, playbackauth.ErrRejected) {
				accessToken = ""
				token, err = playbackToken(accessToken)
			} else if checkErr != nil {
				return nil, false, fmt.Errorf("resolve Twitch playback connection: %w", checkErr)
			}
		}
		if err != nil {
			return nil, false, fmt.Errorf("playback token: %w", err)
		}
	}
	var manifest *twitch.Manifest
	if p.isVOD() {
		manifest, err = s.twitch.FetchVODMasterPlaylist(ctx, p.VODID, token, opts)
	} else {
		manifest, err = s.twitch.FetchMasterPlaylist(ctx, p.BroadcasterLogin, token, opts)
	}
	if err != nil {
		return nil, false, fmt.Errorf("master playlist: %w", err)
	}
	return manifest, accessToken == "", nil
}

func kindFromRecordingType(rt string) remux.Kind {
	if rt == twitch.RecordingTypeAudio {
		return remux.KindAudio
	}
	return remux.KindVideo
}

// isCorrupt ignores unmeasurable durations, including ffprobe's N/A values.
func isCorrupt(r *probe.Result, kind remux.Kind) bool {
	if r == nil || r.Duration == 0 {
		return false
	}
	var streamDur float64
	switch kind {
	case remux.KindAudio:
		if r.AudioStream != nil {
			streamDur = r.AudioStream.Duration
		}
	default:
		if r.VideoStream != nil {
			streamDur = r.VideoStream.Duration
		}
	}
	if streamDur == 0 {
		return false
	}
	return math.Abs(r.Duration-streamDur) > remux.CorruptionThreshold
}

func (s *Service) uploadFromScratch(ctx context.Context, d *download, scratchPath, storagePath string) error {
	return s.uploadScratch(ctx, d, scratchPath, storagePath, "")
}

func (s *Service) uploadScratch(ctx context.Context, d *download, scratchPath, storagePath, digest string) error {
	return s.writeToStorage(ctx, func() error {
		f, err := os.Open(scratchPath)
		if err != nil {
			return fmt.Errorf("open scratch: %w", err)
		}
		defer f.Close()
		owned, err := s.storage.ForAttempt(ctx, d.claim())
		if err != nil {
			return err
		}
		defer owned.Close()
		if digest == "" {
			err = owned.Save(ctx, filepath.ToSlash(storagePath), f)
		} else {
			err = owned.SavePrepared(ctx, filepath.ToSlash(storagePath), f, digest)
		}
		if err != nil {
			return fmt.Errorf("save to storage: %w", err)
		}
		return nil
	})
}

func (s *Service) setResumeStage(dbCtx context.Context, d *download, stage Stage, log *slog.Logger) {
	d.resume.SetStage(stage)
	s.checkpointResume(dbCtx, d, log)
}

// checkpointResume retains scratch and cancels acquisition when a checkpoint cannot be confirmed.
func (s *Service) checkpointResume(dbCtx context.Context, d *download, log *slog.Logger) {
	data, err := json.Marshal(d.resume)
	if err != nil {
		log.Error("resume state marshal failed", "error", err, "stage", d.resume.Stage)
		return
	}
	if err := s.persistCheckpoint(dbCtx, d, data); err != nil {
		d.persistenceErr = err
		if d.cancel != nil {
			d.cancel()
		}
		log.Error("resume state persist failed; preserving recovery", "error", err, "stage", d.resume.Stage)
	}
}

// failDownload settles user cancellation or failure after joining auxiliary workers.
// Shutdown and unresolved persistence preserve the attempt for recovery.
func (s *Service) failDownload(dbCtx context.Context, d *download, log *slog.Logger, cause error) {
	if d.stopChildren != nil {
		d.stopChildren()
	}
	s.mu.Lock()
	userCancelled := d.userCancelled
	s.mu.Unlock()
	userCancelled = userCancelled || errors.Is(cause, ErrCancelled) || errors.Is(cause, repository.ErrStopRequested) || errors.Is(d.persistenceErr, repository.ErrStopRequested)
	if !userCancelled && d.runCtx != nil && errors.Is(context.Cause(d.runCtx), storage.ErrFull) {
		d.persistenceErr = context.Cause(d.runCtx)
	}
	if !userCancelled && d.runCtx != nil && errors.Is(context.Cause(d.runCtx), errExecutionDeferred) {
		s.checkpointResume(dbCtx, d, log)
		s.closeMetadataSpans(dbCtx, d, log, "deferral")
		return
	}
	if !userCancelled && d.persistenceErr != nil {
		log.Error("recording persistence unresolved; preserving recovery", "error", d.persistenceErr)
		return
	}

	if s.shuttingDown.Load() && !userCancelled {
		s.closeMetadataSpans(dbCtx, d, log, "shutdown")
		// Shutdown must retain the saved segments needed for recovery.
		log.Info("download interrupted by shutdown; leaving RUNNING for resume",
			"error", cause,
			"stage", d.resume.Stage,
			"accounted_frontier", d.resume.AccountedFrontierMediaSeq)
		s.checkpointResume(dbCtx, d, log)
		return
	}

	settleCtx := dbCtx
	if d.runCtx != nil {
		settleCtx = d.runCtx
	}
	recorded := cause
	s.closeMetadataSpans(dbCtx, d, log, "failure")
	if userCancelled {
		recorded = ErrCancelled
		log.Info("download cancelled by user")
	} else {
		log.Error("download failed", "error", cause)
	}
	message := recorded.Error()
	if d.vod {
		message = archiveFailureMessage(recorded)
	}
	// A failed classification read cannot prove no saved media exists or permit scratch removal.
	var hasPart bool
	err := s.persist(settleCtx, "classify failure", func(c context.Context) error {
		var err error
		hasPart, err = s.repo.HasFinalizedVideoParts(c, d.videoID)
		return err
	})
	if err != nil {
		log.Error("failure classification unresolved", "error", err)
		return
	}
	partsKnown := true
	failCompletionKind := repository.CompletionKindComplete
	switch {
	case userCancelled:
		failCompletionKind = repository.CompletionKindCancelled
	case partsKnown && hasPart:
		failCompletionKind = repository.CompletionKindPartial
	}
	// Post-capture processing failures do not truncate a broadcast that reached ENDLIST.
	cutShort := userCancelled || d.resume.HadWindowRoll || !d.resume.EndListSeen
	truncated := failedRunTruncated(partsKnown, hasPart, cutShort)

	// A scheduled retry is not terminal, so it must not enqueue a completion webhook.
	if d.vod && !userCancelled {
		if delay, ok := archiveRetryDelay(d.attempt); ok && archiveRetryable(cause) {
			retryAt := time.Now().UTC().Add(delay)
			if err := s.persistVideoChange(settleCtx, "archive retry", func(dbCtx context.Context) error {
				return s.repo.WithTx(dbCtx, func(tx repository.Repository) error {
					if _, err := repository.GuardAttempt(dbCtx, tx, d.claim()); err != nil {
						return err
					}
					if err := tx.MarkArchiveFailedForRetry(dbCtx, d.videoID, message, failCompletionKind, truncated, retryAt); err != nil {
						return err
					}
					return tx.MarkJobFailed(dbCtx, d.jobID, message)
				})
			}); err != nil {
				if errors.Is(err, repository.ErrStopRequested) {
					s.failDownload(dbCtx, d, log, err)
					return
				}
				// Keep the old attempt and its checkpoint recoverable. Neither a
				// retry nor a terminal event exists until both writes commit.
				log.Error("failed to persist archive retry; preserving attempt for recovery", "error", err)
				return
			} else {
				d.cleanupScratch = true
				log.Warn("archive failed; retry scheduled",
					"attempt", d.attempt, "retry_at", retryAt, "error", cause)
				s.publishArchiveQueue(eventbus.ArchiveFailed, d.videoID)
				return
			}
		}
	}

	if err := s.persist(settleCtx, "failure", func(c context.Context) error {
		return s.markRecordingFailed(c, d.claim(), message, failCompletionKind, truncated)
	}); err != nil {
		if errors.Is(err, repository.ErrStopRequested) {
			s.failDownload(dbCtx, d, log, err)
			return
		}
		log.Error("failed to persist recording failure; preserving attempt for recovery", "error", err)
		return
	}
	// Terminal jobs are excluded from recovery, so their scratch can be removed.
	d.cleanupScratch = true
	s.publishRecordingTerminal(d.videoID, eventbus.RecordingFailed)
	if d.vod {
		s.publishArchiveQueue(eventbus.ArchiveFailed, d.videoID)
	}
}

func (s *Service) closeMetadataSpans(dbCtx context.Context, d *download, log *slog.Logger, reason string) {
	if d.vod {
		return
	}
	if err := repository.StopAttemptMetadata(dbCtx, s.repo, d.claim(), time.Now().UTC()); err != nil {
		log.Warn("close video metadata spans on "+reason, "video_id", d.videoID, "error", err)
	}
}

// classifyTwitchAuth reports whether Twitch refused playback permanently.
// Other 401/403 responses may be repaired with a fresh playback token.
func classifyTwitchAuth(status int, body []byte) bool {
	return twitch.IsPermanent(twitch.NewAuthError(status, body))
}

type storageSnapshotWriter struct {
	storage  *mediastore.Store
	claim    repository.AttemptClaim
	filename string
	cursor   int
}

// WriteSnapshot publishes one frame under a fresh key before promoting it to the video thumbnail.
func (w *storageSnapshotWriter) WriteSnapshot(ctx context.Context, index int, body io.Reader) error {
	const maxSnapshotBytes = 8 << 20
	data, err := io.ReadAll(io.LimitReader(body, maxSnapshotBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxSnapshotBytes {
		return fmt.Errorf("snapshot exceeds %d bytes", maxSnapshotBytes)
	}
	owned, err := w.storage.ForAttempt(ctx, w.claim)
	if err != nil {
		return err
	}
	defer owned.Close()
	w.cursor = max(w.cursor, index)
	next, err := owned.NextSnapshotIndex(ctx, w.filename, w.cursor)
	if err != nil {
		return err
	}
	w.cursor = next
	key := storagekeys.Snapshot(w.filename, w.cursor)
	if err := owned.Save(ctx, key, bytes.NewReader(data)); err != nil {
		return err
	}
	w.cursor++
	return owned.Commit(ctx, func(tx repository.Repository) error {
		_, err := tx.SetVideoThumbnailIfMissing(ctx, w.claim.VideoID, key)
		return err
	})
}

// buildFilename includes a job suffix so recordings of the same broadcaster do not collide.
func buildFilename(login, jobID string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	short := strings.ReplaceAll(jobID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("%s-%s-%s", ts, login, short)
}
