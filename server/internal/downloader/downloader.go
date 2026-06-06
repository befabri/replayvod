// Package downloader runs the native Go HLS → ffmpeg pipeline that
// turns a live Twitch stream into a stored MP4. Each download has
// a unique jobID so callers (tRPC handlers, the scheduler, webhook
// handlers) can subscribe to progress updates and request
// cancellation without holding a reference to the in-flight work.
//
// Pipeline composition (spec stages 1-11):
//
//  1. twitch.Client.PlaybackToken            — GQL access token
//  2. twitch.Client.FetchMasterPlaylist      — usher manifest
//  3. twitch.SelectVariant                   — quality/codec pick
//  4. hls.Run                                — segments → scratch
//  5. remux.PrepareInput                     — segments.txt / media.m3u8
//  6. remux.Remuxer.Run                      — ffmpeg → mp4/m4a
//  7. probe.Probe.Run                        — duration + streams
//  8. thumbnail.Generator.Generate           — jpg at 10% (video only)
//  9. corruption check → remux.Remuxer.Heal  — if duration drifts >50s
//  10. storage.Save                          — upload to backend
//  11. os.RemoveAll(work_dir)                — cleanup
//
// Durable state: jobs table (status + resume_state JSONB per attempt)
// plus video_parts (one row per output part — 1..N rows depending on
// whether Twitch dropped the variant mid-stream or the resume gap
// exceeded MaxRestartGapSeconds). Start() creates both; run()
// transitions them alongside the pipeline; Resume() at server boot
// reads RUNNING jobs and re-spawns them.
//
// Shutdown semantics: SIGINT/SIGTERM cancels in-flight jobs' contexts
// but LEAVES their rows as RUNNING so the next Resume() picks them
// back up. A user-initiated Cancel marks the video FAILED explicitly.
package downloader

import (
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

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/downloader/probe"
	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/downloader/thumbnail"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/waveform"
)

// qualityToHeight maps the repository's coarse Quality enum (LOW /
// MEDIUM / HIGH) to the numeric-string form the twitch variant
// selector expects. Unknown quality values default to 1080 — the
// spec's PreferredQuality — so a config drift doesn't silently
// pick an unexpected variant.
func qualityToHeight(q string) string {
	switch q {
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

// Params describes a single download request. RecordingType +
// ForceH264 drive Stage 3 variant selection and land on the video
// row; zero values are the conservative defaults (video + no
// codec-override).
type Params struct {
	BroadcasterID    string
	BroadcasterLogin string
	DisplayName      string
	// Title is the stream title at download-start time. Caller
	// (video.Trigger or schedule.processor) resolves it from
	// Helix GetStreams; empty when no live stream is visible.
	Title string
	// CategoryID is the Twitch game_id the broadcaster had set at
	// download-start time. When non-empty the downloader links it
	// to video_categories after CreateVideo so the video shows up
	// on /dashboard/categories/$id. Empty means Twitch didn't
	// surface a category (off-topic / "just chatting" with no game
	// set). Mid-stream changes are captured via channel.update
	// (webhook mode) or the metadata watcher (poll mode).
	CategoryID string
	// CategoryName accompanies CategoryID for the initial upsert —
	// Hydrator.linkVideoCategory skips the UpsertCategory when
	// Name is empty to protect an existing good name from being
	// clobbered.
	CategoryName string
	Quality      string
	Language     string
	ViewerCount  int64
	StreamID     *string

	// RecordingType is "video" (default) or "audio". Audio jobs
	// pick the audio_only rendition at Stage 3 and produce an
	// .m4a output. Empty defaults to video at the repo layer.
	RecordingType string

	// ForceH264 drops HEVC/AV1 variants before the Stage 3
	// quality-fallback chain. Operator-exposed per the spec's
	// "User codec preference" section.
	ForceH264 bool

	// Schedule-triggered recordings snapshot their retention policy at
	// creation time. Manual recordings leave these nil so later schedule
	// edits cannot retroactively delete them.
	TriggerScheduleID         *int64
	RetentionSourceScheduleID *int64
	RetentionWindowHours      *int64
}

// Progress is the per-segment cumulative snapshot pushed to the
// per-job channel. Shape matches the spec's DownloadProgress so
// the SSE subscriber can render a meaningful progress bar.
//
// Cumulative semantics: each event fully supersedes the previous.
// Intermediate events are safe to drop (buffered channel, non-
// blocking send); the terminal event goes through when the
// bridge closes the chan.
type Progress struct {
	JobID string `json:"job_id"`

	// PartIndex is 1-based and increments on a part boundary
	// (variant/codec/container switch, resume gap, or configured
	// size/duration ceiling). Single-part recordings stay at 1.
	PartIndex int `json:"part_index"`

	// Stage labels the active pipeline stage. Values:
	//   "auth" | "playlist" | "segments" | "remux" |
	//   "metadata" | "thumbnail" | "done"
	Stage string `json:"stage"`

	// BytesWritten is cumulative across parts — the sum of
	// successfully committed segment bytes so far.
	BytesWritten int64 `json:"bytes_written"`

	// SegmentsDone + SegmentsGaps + SegmentsAdGaps +
	// SegmentsTotal track the segment-level counters. Ad-gaps
	// are reported distinctly from quality-gaps so the UI can
	// show "Twitch ad content skipped" separately from "fetch
	// failures tolerated," and so the gap-policy MaxGapRatio
	// doesn't count ads as errors. Total is -1 for a live
	// playlist before EXT-X-ENDLIST; set once the window closes.
	SegmentsDone   int64 `json:"segments_done"`
	SegmentsGaps   int64 `json:"segments_gaps"`
	SegmentsAdGaps int64 `json:"segments_ad_gaps"`
	SegmentsTotal  int64 `json:"segments_total"`

	// Percent is SegmentsDone / SegmentsTotal when Total is
	// known, otherwise -1. The UI renders an indeterminate bar
	// on -1.
	Percent float64 `json:"percent"`

	// Speed is a human-readable bytes/second string (e.g.
	// "2.4 MiB/s"). Empty while the bridge hasn't seen enough
	// deltas to compute a rate. Computed from a short
	// rolling-window average so a one-burst read doesn't
	// spike the display.
	Speed string `json:"speed"`

	// ETA is a human-readable time-to-completion string when
	// SegmentsTotal is known and Speed is positive, otherwise
	// empty.
	ETA string `json:"eta"`

	// Quality + Codec describe the current part's variant.
	// Populated from the twitch.SelectedVariant once Stage 3
	// completes; empty before.
	Quality string   `json:"quality"`
	FPS     *float64 `json:"fps,omitempty"`
	Codec   string   `json:"codec"`

	// RecordingType mirrors the video row — "video" or "audio".
	RecordingType string `json:"recording_type"`

	// MediaOffsetSeconds is the best known exact media time of the
	// running recording. It is set from the downloader resume accounting,
	// not wall-clock time, so dashboard timelines can place live metadata
	// and part markers on the same axis.
	MediaOffsetSeconds *float64 `json:"media_offset_seconds,omitempty"`
}

// Service orchestrates downloads. Safe for concurrent use. One
// Service per process; the pipeline components are constructed
// once in NewService and shared across all jobs.
type Service struct {
	cfg     *config.Config
	repo    repository.Repository
	storage storage.Storage
	log     *slog.Logger

	twitch      *twitch.Client
	fetcher     *hls.Fetcher
	remuxer     *remux.Remuxer
	probe       *probe.Probe
	thumb       *thumbnail.Generator
	waveforms   waveform.Generator
	svcAcct     *serviceAccount
	hydrator    *streammeta.Hydrator
	metaWatcher titleWatcher
	channelSubs ChannelUpdateSubscriber

	mu              sync.Mutex
	active          map[string]*download
	activeSubs      map[int]chan struct{}
	nextActiveSubID int

	// wg tracks the per-job run() goroutines so Shutdown can wait
	// for their defers (resume-state flush, progressCh close,
	// active-map cleanup) to land before the process exits.
	wg sync.WaitGroup

	// shuttingDown flips atomically on Shutdown(). failDownload
	// observes it to suppress the mark-FAILED transition — a
	// job interrupted by shutdown stays RUNNING so Resume() on
	// the next boot picks it back up per spec line 615.
	shuttingDown atomic.Bool

	// bus, when set via SetEventBus, receives a RecordingTerminal wake-up hint
	// after each terminal transition (success or non-shutdown failure). The
	// durable recording-webhook row is written in the same DB transaction as the
	// terminal video update; this bus only nudges the dispatcher to poll now
	// instead of waiting for its next interval.
	bus *eventbus.Buses
}

// download is the per-job state kept in memory. cancel propagates
// a user Cancel() to every stage (playlist, fetch, remux, probe,
// thumbnail) via one shared ctx.
type download struct {
	jobID          string
	videoID        int64
	broadcasterID  string
	cancel         context.CancelFunc
	userCancelled  bool
	progressCh     chan Progress
	startedAt      time.Time
	progressMu     sync.RWMutex
	latestProgress Progress

	// resume is the durable per-job checkpoint. Lives in memory
	// alongside the running pipeline; persisted to
	// jobs.resume_state on every material state transition so a
	// crash-restart can pick up without reprocessing completed
	// work. Zero-valued state (Stage=AUTH) is the "fresh job"
	// shape and is safe to persist as-is.
	resume *ResumeState

	// videoPartID is the row ID of the CURRENT part's video_parts
	// entry — the one runPart is actively populating. Created at
	// Stage 5 (PrepareInput) and finalized at Stage 10 (Store) of
	// each part. Reset to zero on a part boundary so the next
	// runPart pass creates (or finds) its own row.
	videoPartID int64

	// completedMediaDurationSeconds is the sum of finalized parts before the
	// current part. mediaOffset* publishes that base plus
	// resume.PartDurationSeconds to metadata pollers without exposing the
	// mutable ResumeState across goroutines. The value + exactness flag are
	// guarded together so an inexact offset can never be observed with a stale
	// exact=true flag.
	//
	// Note the two clocks: finalized parts contribute container-PROBED duration,
	// but the in-flight current part contributes resume.PartDurationSeconds
	// (sum of #EXTINF). EXTINF and probed duration differ slightly, so the live
	// offset can drift from the probe-based concatenated-playback timeline by up
	// to the current part's EXTINF-vs-probe delta. This is accepted as a
	// best-known live value; sealed/playback offsets are probe-based and
	// authoritative.
	completedMediaDurationSeconds float64
	mediaOffsetMu                 sync.RWMutex
	mediaOffsetSeconds            float64
	mediaOffsetExact              bool

	// cleanupScratch gates the deferred RemoveAll in run(). Flipped
	// true on terminal exits (success, user cancel, non-shutdown
	// failure) and left false on shutdown interrupts so Resume can
	// re-find the scratch segments on next boot. Mutated only from
	// the run() goroutine and failDownload — no locking needed.
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

// refreshMediaOffset republishes the media offset = sum of the durations of
// segments actually written so far. That is the recording's *playback*
// position, which is exactly what the dashboard timeline wants: it scrubs the
// finished recording, whose axis already excludes any dropped content. So a
// tolerated gap (window roll, fetch failure, malformed, stitched ad) does NOT
// make the offset wrong — both the recording and the offset skip that content,
// and the offset stays a more accurate marker than the wall-clock fallback
// (occurred_at - start), which would overcount by the lost duration.
//
// Exactness gates only on pending auth gaps because those are the one gap kind
// that may be refetched later. A successful refetch folds the segment's
// duration back into PartDurationSeconds, retroactively shifting every later
// position — so while one is outstanding the offset is provisional and we let
// the row fall back to wall-clock ordering instead of stamping a soon-to-move
// value.
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

func (d *download) MediaOffsetSeconds() (float64, bool) {
	d.mediaOffsetMu.RLock()
	defer d.mediaOffsetMu.RUnlock()
	if !d.mediaOffsetExact {
		return 0, false
	}
	return d.mediaOffsetSeconds, true
}

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

// ChannelUpdateSubscriber abstracts the per-recording EventSub
// subscribe/unsubscribe pair used by webhook-mode title tracking.
// The downloader only cares whether the call succeeded; the
// concrete eventsub.Service returns a *repository.Subscription
// we don't need here, so main.go passes an adapter that drops
// the return value.
type ChannelUpdateSubscriber interface {
	SubscribeChannelUpdate(ctx context.Context, broadcasterID string) error
	UnsubscribeChannelUpdate(ctx context.Context, broadcasterID, reason string) error
}

// titleWatcher abstracts the poll-mode mid-stream title watcher so the
// title-tracking decision is unit-testable without a live Helix poller.
// *streammeta.MetadataWatcher satisfies it.
type titleWatcher interface {
	Watch(ctx context.Context, broadcasterID string, videoID int64, initial streammeta.WatchInitial)
}

// NewService wires up the pipeline components. The twitch client,
// fetcher, remuxer, probe, and thumbnail generator are all
// process-lifetime singletons — they hold no per-job state.
//
// metaWatcher may be nil — polling disabled in that case, and the
// downloader relies solely on the at-start snapshot stored on
// videos.title. channelSubs may also be nil — webhook mode
// disabled. The recording runs with whichever strategy is wired
// per `cfg.ServerMode`; main.go constructs only the deps the mode needs.
// NewService wires up the pipeline components.
//
// hydrator is used at download-start to link the opening title +
// category onto video_titles / video_categories (via
// LinkInitialVideoMetadata). It's the same Hydrator shared with
// the trigger paths and the MetadataWatcher — one instance, one
// write path, no drift between the "at-start snapshot" and the
// "mid-stream change" writes. May be nil in tests; the initial-
// link step becomes a no-op.
//
// metaWatcher polls Helix in poll mode; channelSubs does
// channel.update EventSub subscribe/unsubscribe in webhook mode.
// Both optional (mode=off or misconfigured → nil).
func NewService(cfg *config.Config, repo repository.Repository, store storage.Storage, hydrator *streammeta.Hydrator, metaWatcher *streammeta.MetadataWatcher, channelSubs ChannelUpdateSubscriber, log *slog.Logger) *Service {
	domainLog := log.With("domain", "downloader")

	tw := twitch.New(twitch.Config{
		ServiceAccountRefreshToken: cfg.Env.ServiceAccountOAuthToken,
	}, domainLog)

	// Shared HTTP client for segment fetches. MaxConnsPerHost is
	// the service-wide cap on concurrent Twitch edge connections;
	// spec Stage 4 sizes it as MaxConcurrent × SegmentConcurrency.
	aggregateHostCap := max(1, cfg.App.Download.MaxConcurrent) * max(1, cfg.App.Download.SegmentConcurrency)
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
		cfg:         cfg,
		repo:        repo,
		storage:     store,
		log:         domainLog,
		twitch:      tw,
		fetcher:     fetcher,
		remuxer:     &remux.Remuxer{Log: domainLog},
		probe:       &probe.Probe{Log: domainLog},
		thumb:       &thumbnail.Generator{Log: domainLog},
		waveforms:   waveform.FFmpegGenerator{},
		svcAcct:     newServiceAccount(cfg.Env.ServiceAccountOAuthToken, domainLog),
		hydrator:    hydrator,
		channelSubs: channelSubs,
		active:      make(map[string]*download),
		activeSubs:  make(map[int]chan struct{}),
	}
	// Only assign the watcher when non-nil: storing a typed-nil
	// *MetadataWatcher into the titleWatcher interface field would make
	// s.metaWatcher != nil true and panic on Watch in poll mode.
	if metaWatcher != nil {
		s.metaWatcher = metaWatcher
	}
	// Scratch-dir sweep is NOT performed here — Resume() owns that
	// step so it can preserve the work dirs of RUNNING jobs before
	// wiping the rest. Callers that don't resume (tests using
	// t.TempDir, CLI tools that never see a crash) can skip
	// Resume without leaking: the temp dir gets cleaned up via
	// the test harness or the OS.
	return s
}

// SetOAuthRefresher wires in the service-account token-exchange
// callback. Must be called after NewService if
// TWITCH_SERVICE_ACCOUNT_REFRESH_TOKEN is set in the environment
// — without a refresher the service account falls back to
// anonymous playback.
//
// The callback typically wraps the Helix client's
// RefreshUserToken. Taken as a narrow interface (TokenRefresher)
// rather than the full client so internal/downloader doesn't
// depend on internal/twitch.
func (s *Service) SetOAuthRefresher(r TokenRefresher) {
	if s.svcAcct != nil {
		s.svcAcct.setRefresher(r)
	}
}

// SetEventBus wires in the eventbus so terminal transitions publish a
// RecordingTerminal event for the outbound webhook dispatcher. Optional, like
// SetOAuthRefresher: leave it unset (e.g. in tests) to disable publishing.
func (s *Service) SetEventBus(bus *eventbus.Buses) {
	s.bus = bus
}

// publishRecordingTerminal fans a terminal-recording wake-up hint out to the
// bus. Non-blocking and nil-safe by contract (Topic.Publish drops on a full
// subscriber buffer); durable delivery no longer depends on this event.
func (s *Service) publishRecordingTerminal(videoID int64, kind eventbus.RecordingTerminalKind) {
	if s.bus == nil || s.bus.RecordingTerminal == nil {
		return
	}
	s.bus.RecordingTerminal.Publish(eventbus.RecordingTerminalEvent{
		VideoID: videoID,
		Kind:    kind,
	})
}

// recordingWebhookDelivery builds the durable outbox payload enqueued in the
// same transaction as a terminal video transition. It can't fail: the message
// id is derived deterministically from (event, video) rather than drawn from the
// RNG, so a terminal transition always has a non-nil row to enqueue. There is no
// path where the video commits DONE/FAILED while its webhook is silently
// dropped.
func (s *Service) recordingWebhookDelivery(videoID int64, event string) *repository.RecordingWebhookDeliveryInput {
	return recordingwebhook.NewTerminalDeliveryInput(event, videoID, time.Now().UTC())
}

// sweepOrphanedTempsExcept removes leftover per-job work
// directories from a previous crash or hard kill. Directories
// whose name (the jobID) is in `protected` are left in place so
// the resume path can reuse their committed segments + init
// segment. Pass nil to wipe everything unconditionally.
//
// Scratch layout: <scratch>/<jobID>/ contains segments/, the
// remuxed mp4, and the thumbnail. One RemoveAll per job dir
// gets everything.
//
// ScratchDir is assumed to be owned by a single Service
// instance. Two Services sharing a ScratchDir would delete each
// other's in-flight job dirs at startup. Operators running more
// than one downloader process (dev-only corner case) must
// configure distinct ScratchDir paths.
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

// Start queues a download and returns the jobID immediately. The
// actual pipeline runs in a goroutine and publishes progress on
// the channel returned by Subscribe(jobID).
//
// Returns ErrBusy if there's already an active download for this
// broadcaster — prevents two copies of the same stream running
// in parallel. The check is enforced at two layers: an in-memory
// scan of s.active (fast path, covers the common case) and a DB
// query against jobs.status IN ('PENDING','RUNNING') (survives a
// process restart that dropped the in-memory map).
// MaxConcurrent is the service-wide cap on in-flight downloads
// (download.max_concurrent, default 2). Exposed so the dashboard can show how
// many of the available slots are in use.
func (s *Service) MaxConcurrent() int {
	if s.cfg.App.Download.MaxConcurrent <= 0 {
		return 2
	}
	return s.cfg.App.Download.MaxConcurrent
}

func (s *Service) Start(ctx context.Context, p Params) (string, error) {
	s.mu.Lock()
	if s.shuttingDown.Load() {
		s.mu.Unlock()
		return "", ErrShuttingDown
	}
	for _, existing := range s.active {
		if existing.broadcasterID == p.BroadcasterID {
			s.mu.Unlock()
			return "", ErrBusy
		}
	}
	maxConcurrent := s.MaxConcurrent()
	if len(s.active) >= maxConcurrent {
		s.mu.Unlock()
		return "", fmt.Errorf("downloader: at max concurrent downloads (%d): %w", maxConcurrent, ErrAtCapacity)
	}

	// DB-level broadcaster idempotency: catches the case where a
	// previous process crashed leaving PENDING/RUNNING rows the
	// in-memory active map no longer knows about. ErrNotFound is
	// the happy path; any other error is a DB problem worth
	// surfacing.
	switch existing, err := s.repo.GetActiveJobByBroadcaster(ctx, p.BroadcasterID); {
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
		broadcasterID: p.BroadcasterID,
		progressCh:    make(chan Progress, 16),
		startedAt:     time.Now(),
		resume:        NewResumeState(),
	}
	runCtx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	s.active[jobID] = d
	s.wg.Add(1)
	s.mu.Unlock()

	cleanupReserved := true
	defer func() {
		if !cleanupReserved {
			return
		}
		cancel()
		s.wg.Done()
	}()

	vid, err := s.repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:                     jobID,
		Filename:                  filename,
		DisplayName:               p.DisplayName,
		Title:                     p.Title,
		Status:                    repository.VideoStatusPending,
		Quality:                   p.Quality,
		BroadcasterID:             p.BroadcasterID,
		StreamID:                  p.StreamID,
		ViewerCount:               p.ViewerCount,
		Language:                  p.Language,
		RecordingType:             p.RecordingType,
		ForceH264:                 p.ForceH264,
		TriggerScheduleID:         p.TriggerScheduleID,
		RetentionSourceScheduleID: p.RetentionSourceScheduleID,
		RetentionWindowHours:      p.RetentionWindowHours,
	})
	if err != nil {
		s.mu.Lock()
		delete(s.active, jobID)
		s.mu.Unlock()
		return "", fmt.Errorf("create video row: %w", err)
	}
	d.videoID = vid.ID

	// Link initial title + category to the video so /dashboard/categories/$id
	// and TitleHistoryButton surface the opening state immediately,
	// without waiting for a webhook/poll-tick write. Shared helper with
	// the channel.update path keeps both writes consistent; best-effort
	// — a link failure logs but doesn't fail the whole pipeline.
	if s.hydrator != nil {
		// No media-offset seed here. The live resolver reports (0, inexact) until
		// run()'s first refreshMediaOffset, so seeding an exact 0 would disagree
		// with any channel.update landing in that window (which falls back to
		// wall-clock → NULL). Leaving it nil keeps the opening row and an early
		// webhook on the same wall-clock axis; at t≈0 the marker lands at ~0 either
		// way, and later changes carry exact offsets once tracking is live.
		if err := s.hydrator.LinkInitialVideoMetadata(ctx, vid.ID, streammeta.ChannelUpdateMeta{
			Title:        p.Title,
			CategoryID:   p.CategoryID,
			CategoryName: p.CategoryName,
		}); err != nil {
			s.log.Warn("link initial video metadata",
				"video_id", vid.ID, "error", err)
		}
	}

	// Job row lives alongside the video row — one per download
	// attempt. Resume-on-restart reads status IN ('PENDING',
	// 'RUNNING') jobs at boot and drives recovery off them.
	if _, err := s.repo.CreateJob(ctx, &repository.JobInput{
		ID:            jobID,
		VideoID:       vid.ID,
		BroadcasterID: p.BroadcasterID,
	}); err != nil {
		s.mu.Lock()
		delete(s.active, jobID)
		s.mu.Unlock()
		// The video row is already committed. Mark it failed so
		// it doesn't stay PENDING forever; the UI will surface
		// the failure.
		// Pre-run failure (couldn't even create the job row);
		// no completion to report. "complete" is the inert default.
		// truncated=false: the broadcast may have still been live, but
		// nothing was captured here — there's no recording to be
		// truncated relative to.
		_ = s.repo.MarkVideoFailed(ctx, vid.ID, fmt.Sprintf("create job row: %v", err), repository.CompletionKindComplete, false)
		return "", fmt.Errorf("create job row: %w", err)
	}

	cleanupReserved = false
	s.notifyActiveChanged()
	go s.run(runCtx, d, p, filename)
	return jobID, nil
}

// Cancel asks the in-flight pipeline to stop. userCancelled is set
// first so the run goroutine's failure handler records ErrCancelled
// rather than "context canceled."
//
// No-op if the jobID isn't active.
func (s *Service) Cancel(jobID string) {
	s.mu.Lock()
	d, ok := s.active[jobID]
	if ok {
		d.userCancelled = true
	}
	s.mu.Unlock()
	if !ok || d.cancel == nil {
		return
	}
	d.cancel()
}

// Subscribe returns the progress channel for a running job. Nil
// when the job has completed or was never started.
func (s *Service) Subscribe(jobID string) <-chan Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.active[jobID]; ok {
		return d.progressCh
	}
	return nil
}

// SubscribeActive notifies callers whenever the aggregate active-download set
// or any running job's latest progress snapshot changes. The payload itself is
// not sent on this channel; callers pair it with ListActiveProgress() to build
// a fresh snapshot list on every notification.
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

// ListActiveProgress returns the latest in-memory progress snapshot for every
// currently-running job, oldest first by in-memory start order. Backing data
// comes from the same emitter that drives video.downloadProgress SSE, so the
// dashboard can query a coherent snapshot without opening N subscriptions.
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

// Shutdown cancels all active downloads and waits up to 30s for
// their run goroutines to flush durable state (final resume-state
// checkpoint, progress channel close, active-map cleanup) before
// returning. Past the timeout in-flight goroutines continue running
// but the caller proceeds with process exit — the dbCtx
// (context.WithoutCancel) path means late checkpoint writes can
// still land if the caller doesn't force-kill immediately.
//
// Jobs interrupted by shutdown stay RUNNING in the DB — spec
// line 625 "Shutdown is not a download failure." Resume() on the
// next process boot picks them back up. A user Cancel() taken
// concurrently with shutdown wins: ErrCancelled still records.
func (s *Service) Shutdown() {
	s.shuttingDown.Store(true)
	s.mu.Lock()
	for _, d := range s.active {
		if d.cancel != nil {
			d.cancel()
		}
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.log.Info("downloader shutdown: all jobs flushed")
	case <-time.After(30 * time.Second):
		s.log.Warn("downloader shutdown: 30s timeout reached; some jobs still in flight")
	}
}

// Resume restores in-flight downloads after a process restart.
// Must be called by the server bootstrap AFTER NewService +
// SetOAuthRefresher and BEFORE the HTTP server starts accepting
// requests — otherwise a concurrent Start() could race with
// resume over the in-memory active map or concurrency cap.
//
// For every jobs row with status IN ('PENDING','RUNNING'):
//
//   - Preserves the job's scratch directory from the orphan sweep
//     (committed segments + init.mp4 on disk get reused).
//   - Loads the video + channel rows to reconstruct Params.
//   - Unmarshals resume_state into *ResumeState.
//   - Spawns run(), which seeds hls.Run's StartMediaSeq from
//     AccountedFrontierMediaSeq+1 so already-committed segments
//     aren't re-fetched.
//
// Job-level failures are recorded on the job row and surfaced to
// the operator; they don't fail Resume overall. A catastrophic
// repo failure (can't list) is the only return-error case.
//
// Safe to call multiple times: jobs already in s.active are
// skipped on subsequent calls.
func (s *Service) Resume(ctx context.Context) error {
	jobs, err := s.repo.ListRunningJobs(ctx)
	if err != nil {
		return fmt.Errorf("list running jobs: %w", err)
	}

	protected := make(map[string]bool, len(jobs))
	for i := range jobs {
		protected[jobs[i].ID] = true
	}
	s.sweepOrphanedTempsExcept(protected)

	if len(jobs) == 0 {
		return nil
	}
	s.log.Info("resuming in-flight jobs", "count", len(jobs))
	for i := range jobs {
		job := jobs[i]
		if err := s.restartJob(ctx, &job); err != nil {
			s.log.Error("resume job failed",
				"job_id", job.ID,
				"video_id", job.VideoID,
				"broadcaster_id", job.BroadcasterID,
				"error", err)
			errMsg := fmt.Sprintf("resume: %v", err)
			_ = s.repo.MarkJobFailed(ctx, job.ID, errMsg)
			// For truncated, mirror the run-time rule: a resume that picks up at
			// REMUX/STORE with the playlist's ENDLIST already observed is a
			// post-broadcast failure, not a live-recording-cut-short. Default to
			// truncated=true if the saved resume_state is unreadable (we can't
			// tell, and "looks incomplete" is the safer loud signal).
			truncated := true
			if state, perr := UnmarshalResumeState(job.ResumeState); perr == nil {
				truncated = state.HadWindowRoll || !state.EndListSeen
			}
			// completion_kind mirrors the run-time failure path: a job that
			// already finalized parts before this failed restart owns
			// reclaimable objects, so stamp it "partial" to keep it inside the
			// retention sweep (which only sees DONE plus FAILED partial/cancelled).
			// Leaving it "complete" would strand those uploaded parts. A repo
			// error keeps the safe "complete" default rather than mis-stamping.
			failKind := repository.CompletionKindComplete
			if hasPart, herr := s.repo.HasFinalizedVideoParts(ctx, job.VideoID); herr != nil {
				s.log.Warn("resume failure: check finalized parts", "video_id", job.VideoID, "error", herr)
			} else if hasPart {
				failKind = repository.CompletionKindPartial
			}
			// A recording that was RUNNING from a prior process and can't be
			// resumed is terminating in failure — exactly what a
			// recording.failed consumer expects to hear. Enqueue the webhook in
			// the same transaction as the FAILED transition (as failDownload
			// does), then wake the dispatcher.
			delivery := s.recordingWebhookDelivery(job.VideoID, recordingwebhook.EventFailed)
			_ = s.repo.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, job.VideoID, errMsg, failKind, truncated, delivery)
			s.publishRecordingTerminal(job.VideoID, eventbus.RecordingFailed)
		}
	}
	return nil
}

// restartJob rebuilds a single download's in-memory state from
// its DB rows + resume_state, inserts it into s.active, and
// spawns run(). Returns an error only for recoverable-looking
// setup failures; run()'s own error path handles anything that
// goes wrong during the pipeline itself.
func (s *Service) restartJob(ctx context.Context, job *repository.Job) error {
	s.mu.Lock()
	if _, exists := s.active[job.ID]; exists {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		return fmt.Errorf("parse resume state: %w", err)
	}

	vid, err := s.repo.GetVideo(ctx, job.VideoID)
	if err != nil {
		return fmt.Errorf("load video: %w", err)
	}
	chn, err := s.repo.GetChannel(ctx, job.BroadcasterID)
	if err != nil {
		return fmt.Errorf("load channel: %w", err)
	}

	p := Params{
		BroadcasterID:    job.BroadcasterID,
		BroadcasterLogin: chn.BroadcasterLogin,
		DisplayName:      vid.DisplayName,
		Title:            vid.Title,
		Quality:          vid.Quality,
		Language:         vid.Language,
		ViewerCount:      vid.ViewerCount,
		StreamID:         vid.StreamID,
		RecordingType:    vid.RecordingType,
		ForceH264:        vid.ForceH264,
	}

	d := &download{
		jobID:         job.ID,
		videoID:       vid.ID,
		broadcasterID: job.BroadcasterID,
		progressCh:    make(chan Progress, 16),
		startedAt:     time.Now(),
		resume:        state,
	}

	runCtx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	s.mu.Lock()
	maxConcurrent := s.MaxConcurrent()
	if len(s.active) >= maxConcurrent {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("at max concurrent downloads (%d); cannot resume", maxConcurrent)
	}
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
		s.notifyActiveChanged()
	}()

	// Shutdown closes open metadata spans regardless of stage (see
	// failDownload's shutdown branch). Reopen on any resume so AUTH,
	// PLAYLIST, or post-BeginNewPart crashes don't permanently strand
	// the prior title/category at zero live duration. The SQL is a
	// no-op when an open span already exists.
	if err := s.repo.ResumeVideoMetadataSpans(ctx, vid.ID, time.Now().UTC()); err != nil {
		return fmt.Errorf("resume video metadata spans: %w", err)
	}

	// vid.Filename is the deterministic base name chosen at
	// original Start(); reuse it so the remuxed path is stable
	// across restart.
	s.wg.Add(1)
	cleanupReserved = false
	s.notifyActiveChanged()
	go s.run(runCtx, d, p, vid.Filename)
	return nil
}

// ErrBusy is returned by Start when a download for the broadcaster
// is already in flight. Callers that want to replace the running
// download should call Cancel first.
//
// ErrAtCapacity is returned by Start when MaxConcurrent downloads are
// already running. Surfaced to the trigger dialog as an actionable message.
//
// ErrCancelled marks a download that was terminated by a user
// Cancel() rather than crashing. Distinguishing matters for the UI.
var (
	ErrBusy         = errors.New("downloader: broadcaster already has an active download")
	ErrShuttingDown = errors.New("downloader: shutting down")
	ErrAtCapacity   = errors.New("downloader: at maximum concurrent downloads")
	ErrCancelled    = errors.New("downloader: cancelled by user")

	// ErrVariantChanged fires when a Stage-3 re-select inside
	// fetchWithAuthRefresh lands on a different (quality, codec)
	// pair than the one locked in for the current part. The outer
	// run() loop catches it alongside hls.ErrPlaylistGone as a
	// part-split trigger, finalizes the current part, and re-
	// enters the loop for a new variant. The error itself stays
	// a sentinel so the inner code paths don't have to know about
	// the part-split policy.
	ErrVariantChanged = errors.New("downloader: selected variant changed mid-run")

	// ErrRestartGapExceeded fires when a resume's first poll
	// observes that the playlist head has rolled past the prior
	// attempt's accounted frontier by more than
	// cfg.Download.MaxRestartGapSeconds. Per spec §"Resume on
	// restart" point 5, sprawling a multi-minute hole inside a
	// single .mp4 is worse than splitting at the boundary —
	// a player can seek across part files but won't gracefully
	// handle a discontinuity that long inside one file.
	//
	// Surfaces from fetchWithAuthRefresh after the OnWindowRoll
	// callback has set d.resume.PendingSplit + cancelled the
	// scoped run context. Treated as a split signal by the outer
	// loop alongside ErrPlaylistGone and ErrVariantChanged.
	ErrRestartGapExceeded = errors.New("downloader: resume gap exceeds MaxRestartGapSeconds; forcing part split")

	// ErrPartThresholdExceeded fires when the current part's
	// accumulated committed bytes or segment duration crosses the
	// operator's MaxPartBytes / MaxPartSeconds ceiling. Unlike the
	// other split signals it is NOT a loss or a variant change — the
	// stream is healthy and continues into the next part at the very
	// next media sequence. The OnEvent accumulator sets
	// PendingSplit + PendingThresholdSplit and cancels the scoped run
	// context (exactly like OnWindowRoll), so the cut lands on a clean
	// segment boundary. isSplitSignal classifies it and the run()
	// loop finalizes the part and opens the next via ContinuePart
	// (seq-continuous), not BeginNewPart (re-anchored).
	ErrPartThresholdExceeded = errors.New("downloader: part exceeded size/duration ceiling; forcing part split")
)

// startTitleTracking begins mid-stream title/category tracking for a recording
// according to the server mode, returning a cleanup func (never nil) the caller
// must defer.
//
// Webhook mode (direct/relay) subscribes to channel.update; the returned cleanup
// unsubscribes. If the subscribe fails there is no poll fallback — poll-mode
// title tracking only exists in poll mode, so the recording keeps just its
// at-start title snapshot. Poll mode launches the Helix-polling watcher and
// registers its cancel via registerPoller so it is torn down with the other
// media pollers after the part loop (the returned cleanup is then a no-op). Off
// mode, or a missing dependency for the active mode, is a no-op.
func (s *Service) startTitleTracking(
	ctx context.Context,
	p Params,
	videoID int64,
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
		// Unsubscribe under WithoutCancel so a recording cancel doesn't strand
		// the Twitch sub; 15s timeout caps a single stuck DELETE so it can't eat
		// Shutdown's 30s budget. Orphans get swept by ReconcileChannelUpdateSubs.
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
		// Initial title + category were already linked at CreateVideo; the
		// watcher gets them as "last seen" so only actual changes record.
		titleCtx, cancelTitle := context.WithCancel(ctx)
		registerPoller(cancelTitle)
		initial := streammeta.WatchInitial{
			Title:       p.Title,
			CategoryID:  p.CategoryID,
			MediaOffset: mediaOffset,
		}
		go func() {
			s.metaWatcher.Watch(titleCtx, p.BroadcasterID, videoID, initial)
			log.Debug("title watcher done")
		}()
	}

	return noop
}

// run walks the full pipeline for one job. All DB writes use
// dbCtx (derived from context.WithoutCancel) instead of the
// runtime ctx so a user Cancel() still lets the "mark failed"
// write land.
func (s *Service) run(ctx context.Context, d *download, p Params, filename string) {
	log := s.log.With("job_id", d.jobID, "broadcaster_login", p.BroadcasterLogin)
	dbCtx := context.WithoutCancel(ctx)

	defer func() {
		close(d.progressCh)
		s.mu.Lock()
		delete(s.active, d.jobID)
		s.mu.Unlock()
		s.notifyActiveChanged()
		s.wg.Done()
	}()

	if err := s.repo.UpdateVideoStatus(dbCtx, d.videoID, repository.VideoStatusRunning); err != nil {
		log.Error("failed to mark video running", "error", err)
	}
	if err := s.repo.MarkJobRunning(dbCtx, d.jobID); err != nil {
		log.Error("failed to mark job running", "error", err)
	}

	// Normalize the recording type early — everything downstream
	// (variant selector, remux kind mapping, progress emitter)
	// keys off it and empty-string would propagate surprises.
	recordingType := p.RecordingType
	if recordingType == "" {
		recordingType = twitch.RecordingTypeVideo
	}
	emitter := newProgressEmitter(d.jobID, recordingType, d.progressCh, func(snap Progress) {
		d.setProgress(snap)
		s.notifyActiveChanged()
	})
	emitter.setMediaOffsetSource(d.MediaOffsetSeconds)

	// Scratch layout: <scratch>/<jobID>/part<NN>/segments/ for
	// fragments + init, <scratch>/<jobID>/<base>-part<NN>.{mp4,jpg}
	// for remux output and thumbs. Uniform across single- and
	// multi-part recordings so storage and resume don't branch.
	//
	// Scratch only gets removed on terminal NON-resumable exits
	// (success, user cancel, fail). Shutdown leaves it for the
	// next process's Resume; orphan sweep cleans up FAILED jobs.
	jobDir := filepath.Join(s.cfg.Env.ScratchDir, d.jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("create scratch dir: %w", err))
		return
	}
	defer func() {
		if !d.cleanupScratch {
			return
		}
		if err := os.RemoveAll(jobDir); err != nil {
			log.Warn("failed to remove scratch", "path", jobDir, "error", err)
		}
	}()

	// Media pollers (snapshot + title) are stream-wide, not
	// part-wide. Stopped once after the outer part loop; deferred
	// stop is the safety net for early-exit paths.
	var mediaPollerCancels []context.CancelFunc
	stopMediaPollers := func() {
		for _, c := range mediaPollerCancels {
			c()
		}
		mediaPollerCancels = nil
	}
	defer stopMediaPollers()

	// Capture Twitch preview frames during recording. The first
	// successful snapshot becomes the row thumbnail immediately.
	// Best-effort; skipped only when no storage backend is wired.
	if s.storage != nil {
		snapCtx, cancelSnap := context.WithCancel(ctx)
		mediaPollerCancels = append(mediaPollerCancels, cancelSnap)
		snapper := thumbnail.NewSnapshotter(thumbnail.SnapshotterConfig{
			Log: log,
		})
		snapWriter := &storageSnapshotWriter{
			storage:  s.storage,
			filename: filename,
			ctx:      ctx,
			onFirstSnapshotSaved: func(path string) {
				if err := s.repo.SetVideoThumbnail(dbCtx, d.videoID, path); err != nil {
					log.Warn("failed to promote first live snapshot to video thumbnail",
						"path", path,
						"error", err,
					)
				}
			},
		}
		go func() {
			count := snapper.Run(snapCtx, p.BroadcasterLogin, snapWriter)
			log.Debug("snapshot ticker done", "captures", count)
		}()
	}

	selectOpts := twitch.SelectOptions{
		RecordingType: recordingType,
		Quality:       qualityToHeight(p.Quality),
		EnableAV1:     s.cfg.App.Download.EnableAV1,
		DisableHEVC:   s.cfg.App.Download.DisableHEVC,
		ForceH264:     p.ForceH264,
	}

	// Pre-load already-finalized parts so MarkVideoDone aggregates
	// the whole recording, not just the still-running part. Empty
	// for fresh jobs.
	var parts []partResult
	var completedSizeBytes int64
	if existingParts, err := s.repo.ListVideoParts(dbCtx, d.videoID); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("list existing video parts: %w", err))
		return
	} else {
		for _, ep := range existingParts {
			if ep.PartIndex >= d.resume.CurrentPartIndex {
				continue
			}
			var thumbRel string
			if ep.Thumbnail != nil {
				thumbRel = *ep.Thumbnail
			}
			parts = append(parts, partResult{
				filename:        ep.Filename,
				durationSeconds: ep.DurationSeconds,
				sizeBytes:       ep.SizeBytes,
				thumbRel:        thumbRel,
			})
			d.completedMediaDurationSeconds += ep.DurationSeconds
			completedSizeBytes += ep.SizeBytes
		}
	}
	if d.resume != nil && d.resume.PartBytes > 0 {
		completedSizeBytes += d.resume.PartBytes
	}
	emitter.seedCompletedBytes(completedSizeBytes)
	d.refreshMediaOffset()

	// Title tracking: webhook subscribes to channel.update EventSub
	// (handler writes on push); poll runs a Helix-polling goroutine; off stores
	// only the at-start title. The poll watcher's cancel is registered with the
	// media pollers so it's torn down after the part loop; the webhook
	// unsubscribe is returned and deferred to run's exit.
	stopTitleTracking := s.startTitleTracking(ctx, p, d.videoID, log, func(cancel context.CancelFunc) {
		mediaPollerCancels = append(mediaPollerCancels, cancel)
	}, d)
	defer stopTitleTracking()

	if d.resume.CurrentPartIndex > 1 {
		emitter.setPart(int(d.resume.CurrentPartIndex))
	}

	// Seeded from durable state so a resume mid-part-N preserves
	// the cross-part window-roll signal for completion_kind.
	hadWindowRoll := d.resume.HadWindowRoll

	for {
		segmentsDir := filepath.Join(jobDir, fmt.Sprintf("part%02d", d.resume.CurrentPartIndex), "segments")
		if err := os.MkdirAll(segmentsDir, 0o755); err != nil {
			s.failDownload(dbCtx, d, log, fmt.Errorf("create part scratch dir: %w", err))
			return
		}
		if recoverLegacySeqZeroPartStarted(segmentsDir, d.resume) {
			log.Info("resume: recovered legacy seq-0 part bootstrap from segment file",
				"part_index", d.resume.CurrentPartIndex,
				"segment_format", d.resume.SegmentFormat)
			s.checkpointResume(dbCtx, d, log)
		}

		// Resume past SEGMENTS skips the fetch entirely: playback
		// tokens have rolled, the stream may be off the wire,
		// and the ENDLIST poll would race the fetch. A persisted
		// PendingSplit while still in SEGMENTS is also a completed
		// fetch for this part: the split decision and boundary were
		// already checkpointed before the crash, so re-entering HLS
		// would append into the part that should be sealed.
		// Synthesize hlsResult from the checkpoint and jump to
		// PrepareInput.
		var hlsResult *hls.JobResult
		if shouldSkipSegmentFetch(d.resume) {
			kind := segmentKindForResume(segmentsDir, d.resume)
			if d.resume.SegmentFormat == "" {
				d.resume.SegmentFormat = string(kind)
				s.checkpointResume(dbCtx, d, log)
			}
			log.Info("resume: skipping segment fetch, checkpoint past SEGMENTS",
				"stage", d.resume.Stage,
				"part_index", d.resume.CurrentPartIndex,
				"accounted_frontier", d.resume.AccountedFrontierMediaSeq,
				"segment_format", d.resume.SegmentFormat,
				"pending_split", d.resume.PendingSplit,
				"pending_threshold_split", d.resume.PendingThresholdSplit)
			hlsResult = synthesizeHLSResultFromResume(d.resume, kind)
		} else {
			s.setResumeStage(dbCtx, d, StageSegments, log)
			var err error
			hlsResult, err = s.fetchWithAuthRefresh(ctx, dbCtx, d, emitter, p, segmentsDir, selectOpts, log)
			if err != nil {
				// hasPartContent is the doom-loop guard: a
				// permanently-broken variant returning split
				// signals against zero-content parts would
				// otherwise loop creating empty rows.
				if isSplitSignal(err) && hasPartContent(hlsResult, d.resume) {
					// Persisted before runPart so a crash
					// mid-Stage-6 still drives part N+1 on
					// resume. Restart-gap splits set this from
					// OnWindowRoll already; idempotent here.
					d.resume.PendingSplit = true
					s.checkpointResume(dbCtx, d, log)
					var segDone int64
					if hlsResult != nil {
						segDone = hlsResult.SegmentsDone
					}
					log.Info("split triggered; opening new part",
						"part_index", d.resume.CurrentPartIndex,
						"segments_done", segDone,
						"prior_frontier", d.resume.AccountedFrontierMediaSeq,
						"reason", err)
				} else if shouldAcceptEmptySplitSignal(len(parts), hlsResult, err) {
					d.resume.PendingSplit = true
					s.checkpointResume(dbCtx, d, log)
					log.Info("split triggered on empty continuation; opening next part without remux",
						"part_index", d.resume.CurrentPartIndex,
						"parts", len(parts),
						"reason", err)
				} else {
					s.failDownload(dbCtx, d, log, err)
					return
				}
			}
			emitter.finalize()
		}

		// Persist ENDLIST only when it proves the whole recording is
		// durably owned by finalized/current parts. A threshold split
		// may observe ENDLIST while concurrent workers have already
		// fetched post-boundary tail segments that this part will prune
		// and the continuation must refetch. In that case EndListSeen
		// stays false until the continuation captures that tail.
		resumeChanged := false
		if hlsResult != nil && hlsResult.EndList {
			if thresholdSplitEndListAtBoundary(d.resume, hlsResult) && !d.resume.PendingSplitEndListAtBoundary {
				d.resume.PendingSplitEndListAtBoundary = true
				resumeChanged = true
			}
			if shouldPersistEndListSeen(d.resume, hlsResult) && !d.resume.EndListSeen {
				d.resume.EndListSeen = true
				resumeChanged = true
			}
		}

		if captureHadWindowRoll(d.resume, &hadWindowRoll) {
			resumeChanged = true
		}

		// The empty-continuation guards decide whether the current part
		// holds real media. They key off currentPartHasCommittedMedia,
		// which consults BOTH this attempt's commits and the durable
		// resume state — so a resumed part whose media was captured before
		// the crash (this run sees only ENDLIST/gaps, SegmentsDone==0) is
		// finalized rather than dropped, and a sealed threshold split (its
		// folded committed bytes show as durable media) needs no special
		// case here. Only a genuinely empty interval reaches a guard.
		sealedThresholdSplit := d.resume.PendingThresholdSplit && d.resume.PendingSplitBoundarySet

		// A continuation part (opened after a split) that captured no
		// segments means the stream ended exactly at the split boundary —
		// e.g. a size/duration ceiling that fired on the final committed
		// segment, or a window roll right as the broadcast closed. The
		// earlier parts already hold the whole recording, so finalize with
		// them. Without this guard the empty trailing part reaches remux,
		// which fails on "no .ts segments" and sinks the entire recording
		// even though every byte was captured. Guarded on len(parts) > 0 so
		// a genuinely empty recording (nothing captured at all) still fails
		// loudly through runPart as before.
		if shouldFinalizeEmptyContinuation(len(parts), hlsResult, d.resume) {
			d.resume.SetStage(StagePrepareInput)
			s.checkpointResume(dbCtx, d, log)
			log.Info("continuation part captured no segments (stream ended at split boundary); finalizing recording",
				"part_index", d.resume.CurrentPartIndex,
				"parts", len(parts))
			break
		}
		if resumeChanged {
			s.checkpointResume(dbCtx, d, log)
		}
		if shouldSkipEmptySplitPart(len(parts), hlsResult, d.resume) {
			log.Info("split continuation resolved no media; opening next part without remux",
				"part_index", d.resume.CurrentPartIndex,
				"parts", len(parts),
				"pending_threshold_split", d.resume.PendingThresholdSplit)
			advanced, err := s.reanchorCurrentPartAfterEmptySplit(dbCtx, d, emitter, log)
			if err != nil {
				s.failDownload(dbCtx, d, log, err)
				return
			}
			if !advanced {
				break
			}
			continue
		}
		if len(parts) > 0 && !currentPartHasCommittedMedia(hlsResult, d.resume) {
			cause := ctx.Err()
			if cause == nil {
				cause = errors.New("continuation part captured no segments before ENDLIST")
			}
			s.failDownload(dbCtx, d, log, cause)
			return
		}

		if sealedThresholdSplit {
			if err := pruneSegmentsAfterBoundary(segmentsDir, hlsResult.Kind, d.resume.PendingSplitBoundaryMediaSeq); err != nil {
				s.failDownload(dbCtx, d, log, fmt.Errorf("prune threshold split tail: %w", err))
				return
			}
		}

		pr, err := s.runPart(ctx, dbCtx, d, p, filename, segmentsDir, hlsResult, emitter, log)
		if err != nil {
			s.failDownload(dbCtx, d, log, err)
			return
		}
		parts = append(parts, *pr)
		d.completedMediaDurationSeconds += pr.durationSeconds
		// Use setMediaOffset, NOT refreshMediaOffset: the part just sealed but
		// resume.PartDurationSeconds isn't reset until continueAfterPendingSplit
		// below, so refreshMediaOffset would add it on top of the already-folded
		// completedMediaDurationSeconds and double-count. Any refreshMediaOffset
		// call inserted between here and that reset would reintroduce the bug.
		d.setMediaOffset(d.completedMediaDurationSeconds, len(d.resume.AuthGapSeqs()) == 0)

		if pendingSplitEndedAtBoundary(d.resume, hlsResult) {
			d.resume.ClearPendingSplit()
			s.checkpointResume(dbCtx, d, log)
			break
		}

		advanced, err := s.continueAfterPendingSplit(dbCtx, d, emitter, log)
		if err != nil {
			s.failDownload(dbCtx, d, log, err)
			return
		}
		if !advanced {
			break
		}
	}

	// Live-capture metadata spans stop when segment acquisition ends,
	// not when the later remux/upload tail finishes. Stop the poll
	// writers first so the close-now timestamp isn't raced by a final
	// tick that would reopen a span against the just-closed state.
	// Webhook-delivered channel.update events still run under
	// WithoutCancel in the event processor and can't be cancelled
	// here — that residual race is closed by MarkVideoDone's
	// internal span close on the terminal transition.
	stopMediaPollers()
	if err := s.repo.CloseOpenVideoMetadataSpans(dbCtx, d.videoID, time.Now().UTC()); err != nil {
		log.Warn("close video metadata spans", "video_id", d.videoID, "error", err)
	}

	// First non-empty thumbRel wins (not just parts[0]) so a
	// monochrome part 1 doesn't strand the video without a hero.
	var aggDuration float64
	var aggSize int64
	var thumbPtr *string
	for _, pr := range parts {
		aggDuration += pr.durationSeconds
		aggSize += pr.sizeBytes
		if thumbPtr == nil && pr.thumbRel != "" {
			t := pr.thumbRel
			thumbPtr = &t
		}
	}
	if repository.NormalizeRecordingType(recordingType) == repository.RecordingTypeAudio {
		if err := s.persistAudioWaveform(ctx, d.videoID, filename, recordingType, aggDuration, parts); err != nil {
			log.Warn("audio waveform artifact generation failed; watch page can rebuild it later",
				"video_id", d.videoID, "error", err)
		}
	}

	// Other gap reasons (stitched-ad, fetch-failure, malformed)
	// are tolerant losses, not interruptions, so don't classify
	// as partial. Only restart_window_rolled does.
	completionKind := repository.CompletionKindComplete
	if hadWindowRoll {
		completionKind = repository.CompletionKindPartial
	}
	// truncated: did the recording stop before the broadcast did? A
	// window roll always implies yes — the CDN rolled because the
	// stream kept going. EndListSeen=false implies yes too — the
	// playlist never closed, so something below the broadcast level
	// (ffmpeg cap, manual stop) ended us early. EndListSeen=true with
	// no window roll is the only "captured the whole broadcast" path.
	truncated := hadWindowRoll || !d.resume.EndListSeen
	delivery := s.recordingWebhookDelivery(d.videoID, recordingwebhook.EventCompleted)
	if err := s.repo.MarkVideoDoneAndEnqueueRecordingWebhook(dbCtx, d.videoID, aggDuration, aggSize, thumbPtr, completionKind, truncated, delivery); err != nil {
		log.Error("failed to mark video done", "error", err)
		return
	}
	if err := s.repo.MarkJobDone(dbCtx, d.jobID); err != nil {
		log.Error("failed to mark job done", "error", err)
		// Job row stuck as RUNNING is a DB-consistency smell
		// but the video output is already committed and
		// uploaded — no value in surfacing this to the user.
	}
	// Terminal success: scratch can be removed. Set the flag
	// before the defer fires on function return.
	d.cleanupScratch = true
	emitter.setStage("done")
	log.Info("download complete",
		"parts", len(parts),
		"duration_seconds", aggDuration,
		"size_bytes", aggSize,
	)
	// Terminal success: wake the webhook dispatcher after the durable outbox row
	// is committed. A dropped publish only delays polling.
	//
	// The single-file playback artifact is NOT built here. Concatenating every
	// finished recording would burn ffmpeg + disk on the many VODs nobody opens;
	// instead the build is kicked lazily the first time someone plays the
	// recording (see StreamHandler.streamPart), so only watched videos cost
	// anything.
	s.publishRecordingTerminal(d.videoID, eventbus.RecordingCompleted)
}

// partResult carries the per-part bookkeeping that runPart hands
// back to run() so the video-level MarkVideoDone gets aggregate
// duration/size and a hero thumbnail. Only the fields run() sums
// or selects from live here — anything internal to a part (probe
// result, remuxed path, video_parts row ID) stays scoped to
// runPart.
type partResult struct {
	filename        string
	localPath       string
	durationSeconds float64
	sizeBytes       int64
	thumbRel        string // storage-relative thumbnail path; "" when none
}

func (s *Service) persistAudioWaveform(ctx context.Context, videoID int64, filename, recordingType string, totalDuration float64, parts []partResult) error {
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
	plan, ok := waveform.BuildPlan(videoID, recordingType, videoDuration, partInputs)
	if !ok {
		return nil
	}
	resp, err := waveform.Generate(ctx, s.waveforms, waveform.InputResolver{
		Storage:    s.storage,
		LocalFiles: localFiles,
	}, plan)
	if err != nil {
		return err
	}
	return waveform.SaveArtifact(ctx, s.storage, storagekeys.Waveform(filename), plan.Fingerprint, resp)
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

	// A size/duration split is a clean cut in one continuous
	// stream, so the next part continues the same MEDIA-SEQUENCE
	// space (ContinuePart: frontier carried to endSeq+1, variant
	// lock kept). Every other split is a genuine discontinuity —
	// a new variant or a rolled window owning an independent
	// counter — so it re-anchors from scratch (BeginNewPart).
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

// isSplitSignal reports whether err means "finalize the current
// part and open a new one." Three surface forms:
//
//   - hls.ErrPlaylistGone   — Twitch 404'd the media playlist
//     URL (variant loss mid-stream, spec §"Variant loss
//     mid-stream").
//   - ErrVariantChanged     — a Stage-3 re-select inside
//     fetchWithAuthRefresh resolved to a different (quality,
//     codec) than what was locked in for the current part.
//   - ErrRestartGapExceeded — a resume's first poll observed a
//     window roll larger than cfg.Download.MaxRestartGapSeconds
//     and the OnWindowRoll callback forced a part split (spec
//     §"Resume on restart" point 5).
//
// run()'s outer loop combines this with hasPartContent() before
// treating it as a split — otherwise a permanently-broken
// variant chain would loop forever creating empty parts.
func isSplitSignal(err error) bool {
	return errors.Is(err, hls.ErrPlaylistGone) ||
		errors.Is(err, ErrVariantChanged) ||
		errors.Is(err, ErrRestartGapExceeded) ||
		errors.Is(err, ErrPartThresholdExceeded)
}

// mapForcedSplitErr translates an in-attempt split callback into a
// split sentinel. OnWindowRoll and threshold OnEvent both set their
// `fired` flag and cancel the scoped run context; hls.Run then often
// returns nil or context.Canceled.
//
// The parent-ctx guard comes first: a parent-ctx cancel means shutdown
// or user-cancel won the race, and the split intent was already
// checkpointed for resume — so the sentinel must NOT be synthesized, or
// it would mask the real teardown.
//
// boundarySealed is the discriminator between the two split kinds, keyed
// on the actual invariant rather than the sentinel's identity. A
// size/duration threshold seals an EXACT, fully-resolved frontier
// boundary: every seq <= boundary is durable, so any residual error is
// the scoped cancel or above-boundary worker/auth/gap work the
// continuation refetches — the sentinel wins unconditionally. A
// restart-gap (window-roll) split has no sealed boundary and only proves
// the scoped cancel won, so a genuine non-cancel error there is real and
// must surface. Keying on boundarySealed (not "is this the threshold
// sentinel") means a future threshold path that ever fires WITHOUT
// sealing degrades safely to the conservative cancel-only rule instead
// of silently masking a real failure.
func mapForcedSplitErr(ctx context.Context, err error, fired, boundarySealed bool, sentinel error, msg string) error {
	if !fired || ctx.Err() != nil {
		return err
	}
	if boundarySealed || err == nil || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %s", sentinel, msg)
	}
	return err
}

// fpsEqual treats both-nil as equal and compares raw values otherwise.
// Twitch advertises declared frame rates as whole numbers (30, 60) and
// rounded decimals (29.970, 59.940); exact equality is fine for our
// variant-lock semantics — a real FPS change crosses a clean boundary.
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

// partEndMediaSeq returns the media-sequence boundary that belongs to
// the finalized part. Threshold splits persist an exact boundary and
// ignore/prune any above-boundary in-flight results, so that boundary
// wins over hlsResult.LastMediaSeq. Other paths keep the historical
// max(last, frontier) behavior.
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
	return resume.Stage.AtOrAfter(StagePrepareInput) || resume.PendingSplit
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

func recoverLegacySeqZeroPartStarted(segmentsDir string, resume *ResumeState) bool {
	if resume.PartStarted ||
		resume.Stage != StageSegments ||
		resume.PartStartMediaSequence != 0 ||
		resume.AccountedFrontierMediaSeq != 0 ||
		len(resume.CompletedAboveFrontier) > 0 ||
		len(resume.Gaps) > 0 {
		return false
	}
	if hasSegmentFile(segmentsDir, "0.ts") {
		resume.PartStarted = true
		if resume.SegmentFormat == "" {
			resume.SegmentFormat = string(hls.SegmentKindTS)
		}
		return true
	}
	if hasSegmentFile(segmentsDir, "0.m4s") {
		resume.PartStarted = true
		if resume.SegmentFormat == "" {
			resume.SegmentFormat = string(hls.SegmentKindFMP4)
		}
		return true
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
		hlsResult.EndList &&
		!currentPartHasCommittedMedia(hlsResult, resume)
}

func shouldSkipEmptySplitPart(priorParts int, hlsResult *hls.JobResult, resume *ResumeState) bool {
	// "Empty" means the current part holds no real media — neither this
	// attempt NOR the durable resume state. A sealed threshold split or a
	// resumed part with prior on-disk segments reports committed media via
	// currentPartHasCommittedMedia and finalizes through runPart; only an
	// ad-only / window-roll-only interval (frontier advanced purely by
	// gaps) is a true empty split that re-anchors without remux.
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

// resumePartHasCommittedMedia reports whether the durable resume state
// already holds at least one committed (non-gap) segment for the current
// part — media on disk even when THIS hls attempt committed nothing new
// (SegmentsDone==0 on a resume whose poll saw ENDLIST or only gaps after
// the crash). An ad-only / window-roll-only interval advances the
// frontier purely through gaps, so span == gap count and this reports
// false; the empty-continuation guards then still treat it as empty.
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

// currentPartHasCommittedMedia is the resume-aware "this part holds real
// media" check the empty-continuation guards use: true when either this
// attempt committed a segment OR the durable state already does. Keying
// the guards off only hlsResult.SegmentsDone drops a resumed part whose
// media was captured before the crash (and lets scratch cleanup delete
// it). It also subsumes the sealed-threshold special case — a sealed
// threshold split always folded committed bytes, so it reports true.
func currentPartHasCommittedMedia(hlsResult *hls.JobResult, resume *ResumeState) bool {
	return hasCommittedMedia(hlsResult) || resumePartHasCommittedMedia(resume)
}

func captureHadWindowRoll(resume *ResumeState, hadWindowRoll *bool) bool {
	before := resume.HadWindowRoll
	if !*hadWindowRoll {
		for _, g := range resume.Gaps {
			if g.Reason == GapReasonRestartWindowRolled {
				*hadWindowRoll = true
				break
			}
		}
	}
	if *hadWindowRoll {
		resume.HadWindowRoll = true
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

func pruneSegmentsAfterBoundary(dir string, kind hls.SegmentKind, boundary int64) error {
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
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
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

// shouldForceSplitOnRestartGap: the lost wall-clock time of a
// window-roll range exceeds the operator's threshold AND the
// current part has content to finalize. thresholdSeconds == 0
// disables (treated as "never split").
func shouldForceSplitOnRestartGap(from, to int64, targetDuration time.Duration, thresholdSeconds int, resume *ResumeState) bool {
	if thresholdSeconds <= 0 {
		return false
	}
	lost := time.Duration(to-from+1) * targetDuration
	threshold := time.Duration(thresholdSeconds) * time.Second
	return lost > threshold && hasPartContent(nil, resume)
}

// hasPartContent: PartStarted is the doom-loop guard. HLS media
// sequences can legitimately start at 0, so PartStartMediaSequence
// cannot double as a presence bit; after BeginNewPart PartStarted is
// false until OnFirstPoll anchors the new part.
func hasPartContent(hlsResult *hls.JobResult, resume *ResumeState) bool {
	if hlsResult != nil && hlsResult.SegmentsDone > 0 {
		return true
	}
	return resume.PartStarted &&
		resume.AccountedFrontierMediaSeq >= resume.PartStartMediaSequence
}

// runPart executes Stages 5-10 for one part. Called from run()'s
// outer part loop; takes the per-part inputs (segmentsDir,
// hlsResult, the resume-state-tracked variant fields) and returns
// the bits the video-level aggregation needs.
//
// Does NOT call MarkVideoDone / MarkJobDone or flip cleanupScratch —
// those are video-wide terminal transitions that fire once after
// the loop completes, regardless of how many parts produced this
// video.
func (s *Service) runPart(ctx, dbCtx context.Context, d *download, p Params,
	filename string, segmentsDir string, hlsResult *hls.JobResult,
	emitter *progressEmitter, log *slog.Logger) (*partResult, error) {

	// segmentsDir is <scratch>/<jobID>/partNN/segments — the
	// remux output and per-part artifacts live two levels up at
	// <scratch>/<jobID>/. Recovering jobDir from the path keeps
	// runPart's signature small without re-deriving it from
	// service config.
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
	// SegmentFormat is now known; mirror it into resume state so
	// a restart rebuilds the right ffmpeg input shape without
	// re-polling the playlist just to learn ts vs fmp4.
	d.resume.SegmentFormat = string(hlsResult.Kind)

	// video_parts row goes in at PREPARE_INPUT so a restart mid-
	// pipeline finds the part metadata already persisted.
	// FinalizeVideoPart at Stage 10 fills in duration/size/
	// thumbnail/end_media_seq — the numbers we only know once
	// probe runs. On resume the row may already exist from the
	// prior attempt; look up by (video_id, part_index) first
	// rather than relying on CreateVideoPart to be idempotent at
	// the adapter layer (it isn't — DB unique constraint would
	// fail).
	if existing, err := s.repo.GetVideoPartByIndex(dbCtx, d.videoID, partIndex); err == nil && existing != nil {
		d.videoPartID = existing.ID
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("lookup video part: %w", err)
	} else {
		part, err := s.repo.CreateVideoPart(dbCtx, &repository.VideoPartInput{
			VideoID:       d.videoID,
			PartIndex:     partIndex,
			Filename:      partFilename + kind.OutputExt(),
			Quality:       d.resume.SelectedQuality,
			FPS:           d.resume.SelectedFPS,
			Codec:         d.resume.SelectedCodec,
			SegmentFormat: d.resume.SegmentFormat,
			StartMediaSeq: d.resume.PartStartMediaSequence,
		})
		if err != nil {
			return nil, fmt.Errorf("create video part: %w", err)
		}
		d.videoPartID = part.ID
	}

	// Stage 5: prepare ffmpeg input. Idempotent; a crash after
	// this but before REMUX just rebuilds the same segments.txt
	// / media.m3u8 on restart.
	s.setResumeStage(dbCtx, d, StagePrepareInput, log)
	emitter.setStage("remux")
	inputPath, err := remux.PrepareInput(segmentsDir, remuxMode)
	if err != nil {
		return nil, fmt.Errorf("remux prep: %w", err)
	}

	// Stage 6: remux. Also idempotent — Remuxer writes through a
	// .part/rename so a crash leaves the previous attempt's
	// output (or nothing) rather than a half-written file.
	s.setResumeStage(dbCtx, d, StageRemux, log)
	remuxIn := remux.RunInput{
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

	// Stage 7: probe.
	s.setResumeStage(dbCtx, d, StageProbe, log)
	emitter.setStage("metadata")
	probeResult, err := s.probe.Run(ctx, remuxedPath)
	if err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}

	// Stage 9: corruption check + heal. If duration mismatch is
	// within tolerance we skip entirely. On heal failure we keep
	// the un-healed file per spec ("partial VOD is better than
	// none").
	if isCorrupt(probeResult, kind) {
		s.setResumeStage(dbCtx, d, StageCorruptionCheck, log)
		log.Info("duration mismatch — running heal pass",
			"part_index", partIndex,
			"format_duration", probeResult.Duration,
			"threshold", remux.CorruptionThreshold)
		healedPath := filepath.Join(jobDir, partFilename+".healed"+kind.OutputExt())
		if err := s.remuxer.Heal(ctx, remuxedPath, healedPath, kind); err != nil {
			log.Warn("heal failed; keeping un-healed file", "error", err)
		} else if healedResult, probeErr := s.probe.Run(ctx, healedPath); probeErr != nil {
			log.Warn("re-probe of healed file failed; keeping un-healed", "error", probeErr)
			_ = os.Remove(healedPath)
		} else if isCorrupt(healedResult, kind) {
			log.Warn("heal did not resolve corruption; keeping un-healed")
			_ = os.Remove(healedPath)
		} else {
			if err := os.Rename(healedPath, remuxedPath); err != nil {
				log.Warn("heal-rename failed; keeping un-healed", "error", err)
				_ = os.Remove(healedPath)
			} else {
				probeResult = healedResult
			}
		}
	}

	// Stage 8: thumbnail + sprite strip. Audio jobs skip both —
	// there's no frame to capture. Per-part thumbnails get the
	// same -partNN suffix as the video; the dashboard hero uses
	// part 01's, the rest live under their own part rows for
	// future per-part UI.
	var thumbRel string
	var stripRel string
	if kind == remux.KindVideo {
		s.setResumeStage(dbCtx, d, StageThumbnail, log)
		emitter.setStage("thumbnail")
		thumbPath := filepath.Join(jobDir, partFilename+".jpg")
		err := s.thumb.Generate(ctx, thumbnail.Input{
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

		// Sprite strip is best-effort. A failure here (bad
		// filter arg on a future ffmpeg, disk full, etc.)
		// shouldn't sink a successful recording — the UI falls
		// back to the single hero thumbnail when the strip is
		// absent.
		if probeResult.Duration > 0 {
			stripPath := filepath.Join(jobDir, partFilename+"-strip.jpg")
			if err := s.thumb.GenerateStrip(ctx, thumbnail.StripInput{
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

	// Stage 10: store. Video first, then thumbnails — if the
	// auxiliary thumbnails fail to upload we still want the
	// video playable.
	s.setResumeStage(dbCtx, d, StageStore, log)
	videoRel := storagekeys.Video(partFilename + kind.OutputExt())
	if err := s.uploadFromScratch(ctx, remuxedPath, videoRel); err != nil {
		return nil, fmt.Errorf("upload video: %w", err)
	}
	var thumbPtr *string
	if thumbRel != "" {
		thumbPath := filepath.Join(jobDir, partFilename+".jpg")
		if err := s.uploadFromScratch(ctx, thumbPath, thumbRel); err != nil {
			log.Warn("thumbnail upload failed; continuing without thumbnail", "error", err)
		} else {
			thumbPtr = &thumbRel
		}
	}
	if stripRel != "" {
		stripPath := filepath.Join(jobDir, partFilename+"-strip.jpg")
		if err := s.uploadFromScratch(ctx, stripPath, stripRel); err != nil {
			log.Warn("strip upload failed; continuing without strip", "error", err)
		}
	}

	// Finalize the part row. Video-level marks (MarkVideoDone /
	// MarkJobDone) live in run() so they fire once after all
	// parts complete.
	if err := s.repo.FinalizeVideoPart(dbCtx, &repository.VideoPartFinalize{
		ID:              d.videoPartID,
		DurationSeconds: probeResult.Duration,
		SizeBytes:       probeResult.Size,
		Thumbnail:       thumbPtr,
		EndMediaSeq:     partEndMediaSeq(hlsResult, d.resume),
	}); err != nil {
		log.Error("failed to finalize video part",
			"part_index", partIndex,
			"error", err)
		// Continue: the upload landed, so the file is playable
		// from the part row even if duration/size weren't
		// updated. A consistency-repair task can backfill from
		// the on-disk file later.
	}

	log.Info("part complete",
		"part_index", partIndex,
		"duration_seconds", probeResult.Duration,
		"size_bytes", probeResult.Size,
		"segments", hlsResult.SegmentsDone,
		"gaps", hlsResult.SegmentsGaps,
	)

	out := &partResult{
		filename:        partFilename + kind.OutputExt(),
		localPath:       remuxedPath,
		durationSeconds: probeResult.Duration,
		sizeBytes:       probeResult.Size,
	}
	if thumbPtr != nil {
		out.thumbRel = *thumbPtr
	}
	return out, nil
}

// fetchWithAuthRefresh runs Stages 1-4 (twitch playback token +
// master playlist + variant selection + hls.Run) with an auth-
// refresh loop around the hls fetch. On hls.ErrPlaylistAuth we
// re-run Stages 1-3 for a fresh signed URL and call hls.Run
// again with StartMediaSeq set to the previous attempt's cursor,
// so segments already on disk aren't re-fetched.
//
// Bounded by cfg.Download.AuthRefreshAttempts. Permanent auth
// failures (entitlement codes) from the Twitch classifier bail
// on the first attempt; retryable auth goes through the budget.
// Non-auth hls errors (gap policy abort, transport exhausted
// on the playlist) surface immediately.
//
// Returns an accumulated JobResult across all attempts. The
// Kind + InitURI are taken from the final successful iteration
// (or the last one attempted on failure).
//
// Gap policy is evaluated PER ATTEMPT, not against the aggregate.
// If attempt 1 commits 99 segments + 1 gap (1%) and auth-refreshes,
// attempt 2 starts its own first-content guard and MaxGapRatio
// check from zero. This is deliberate: a new signed URL is a
// fresh starting point for "has Twitch let us capture anything
// partThresholdAccountant funnels every frontier-advancing outcome
// through one place that records the durable per-part accounting AND
// evaluates the size/duration ceiling in the same call. Routing all
// advances through it is the structural guard against the missed-split
// class of bug (both the window-roll fold and a forgotten OnEvent case):
// a new advance path physically cannot record progress via commit/gap/
// recordRangeGap without the ceiling check riding along. The lone
// exception is authGap, which is deliberately ceiling-free.
//
// commit and gap seal eagerly (OnEvent has no competing split). The
// window-roll path uses recordRangeGap + sealIfCrossed split apart,
// because a restart-gap discontinuity split takes precedence and must be
// weighed between recording the gap and sealing a threshold cut.
type partThresholdAccountant struct {
	resume     *ResumeState
	maxBytes   int64
	maxSeconds int
	// onSeal fires the forced-split side effects (mark fired, log,
	// checkpoint, cancel the scoped run ctx) once a boundary is sealed.
	onSeal func(boundary int64)
}

func (a *partThresholdAccountant) commit(seq, bytes int64, dur float64) {
	a.sealIfCrossed(a.resume.NoteCommittedSegmentUntilThreshold(seq, bytes, dur, a.maxBytes, a.maxSeconds))
}

func (a *partThresholdAccountant) gap(seq int64, reason GapReason) {
	a.sealIfCrossed(a.resume.NoteGapUntilThreshold(seq, reason, a.maxBytes, a.maxSeconds))
}

// authGap records an auth-errored seq as a plain resume gap WITHOUT the
// ceiling check. Auth seqs carry no bytes/duration of their own and the
// next auth-refresh attempt refetches them; a size/duration cut here
// would seal them below a boundary (permanent hole) instead of letting
// the refresh fill them. The deliberate exception to the chokepoint.
func (a *partThresholdAccountant) authGap(seq int64) {
	a.resume.NoteGap(seq, GapReasonAuth)
}

// recordRangeGap fills a lost range (window roll), folding any buffered
// above-frontier commits, and returns the crossing boundary WITHOUT
// sealing — the caller weighs the restart-gap split first, then calls
// sealIfCrossed.
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

// real yet" — it doesn't inherit attempt 1's success/gap ratio.
// Aggregate counters on the returned JobResult are for the
// caller's reporting, not for policy decisions.
func (s *Service) fetchWithAuthRefresh(ctx, dbCtx context.Context, d *download, emitter *progressEmitter, p Params, segmentsDir string, selectOpts twitch.SelectOptions, log *slog.Logger) (*hls.JobResult, error) {
	maxAuthAttempts := s.cfg.App.Download.AuthRefreshAttempts
	if maxAuthAttempts <= 0 {
		maxAuthAttempts = 2
	}

	agg := &hls.JobResult{}
	var authAttempts int
	unresolvedCanceled := map[int64]bool{}

	// refetchSeqs carries forward the prior attempt's auth-errored
	// seqs so the next Poller re-emits them under the fresh URL.
	// Replaced (not appended) per iteration: a successful refetch
	// drops the seq off the list, a re-failed refetch puts it
	// back. Seqs that roll off the CDN window are dropped by the
	// Poller with a warning and stay as GapReasonAuth in resume.
	//
	// First-iteration seed comes from resume state: a process
	// crash between iter 1 (gap recorded) and iter 2 (refetch)
	// would otherwise lose the intent. A resumed job pre-loads
	// its pending auth gaps here; fresh jobs start with nil.
	refetchSeqs := d.resume.AuthGapSeqs()

	// bootstrapped guards PartStartMediaSequence: first poll's
	// MediaSequenceBase anchors the frontier. Auth-refresh
	// iterations reuse the anchor — d.resume is shared across
	// attempts, so a refresh mid-stream doesn't reset the part.
	// A resumed job enters already bootstrapped from its prior
	// attempt's state; fresh jobs bootstrap on the first poll.
	bootstrapped := d.resume.PartStarted

	// Seed startSeq from the resume frontier when we're picking
	// up a prior attempt — the first hls.Run call then skips
	// already-committed segments. Fresh jobs start at 0 (emit
	// everything the playlist publishes).
	var startSeq int64
	if bootstrapped {
		startSeq = d.resume.AccountedFrontierMediaSeq + 1
	}

	// eventsSinceCheckpoint counts OnEvent firings between resume-
	// state writes. Checkpoint cadence: every N events keeps DB
	// traffic bounded during live recording (with 4 workers +
	// ~2s target duration, ~2 events/sec → 1 checkpoint/5s).
	const checkpointEveryEvents = 10
	var eventsSinceCheckpoint int

	for {
		// Stages 1-3: fresh signed URL.
		emitter.setStage("auth")
		variant, err := s.resolveVariantURL(ctx, p, selectOpts)
		if err != nil {
			// Any resolveVariantURL failure — permanent entitlement
			// or transient — bails the whole loop. The loop's
			// purpose is to re-run Stages 1-3 when HLS segment
			// fetch surfaces an auth error; it does not re-run
			// on a Stage 1-3 failure itself. Auth-refresh budget
			// is intentionally not consumed here so a flaky GQL
			// call doesn't burn a retry slot before hls.Run even
			// starts.
			return agg, err
		}
		// Variant lock across auth-refresh iterations: an in-
		// flight pipeline must not silently change codec,
		// container, or quality within a part — `ffmpeg -c copy`
		// across those boundaries produces a broken output. If
		// Stage 3 returns a different variant than the one
		// locked in (either from a prior auth-refresh iteration
		// in THIS run or from a resumed ResumeState), surface
		// ErrVariantChanged. The outer run() loop reads it as a
		// part-split signal: finalize this part, BeginNewPart,
		// re-run Stage 3 from scratch in the new part.
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
		if err := s.repo.UpdateVideoSelectedVariant(dbCtx, d.videoID, variant.Quality, variant.FPS); err != nil {
			return agg, fmt.Errorf("persist selected variant: %w", err)
		}
		// Mirror the selected variant into resume state so a
		// crash-restart between PREPARE_INPUT and STORE recovers
		// the exact (quality, codec) pair without re-walking
		// Stage 3. SegmentFormat lands after hls.Run returns —
		// it's a property of the media playlist, not the master.
		d.resume.SelectedQuality = variant.Quality
		d.resume.SelectedFPS = variant.FPS
		d.resume.SelectedCodec = variant.Codec

		// Stage 4: segment fetch. The progress channel is
		// per-attempt because hls.Run closes it on the way
		// out; sharing across attempts would send on a closed
		// channel.
		//
		// Buffered higher than the downloader-facing progressCh
		// (16) because hls emits per-segment, which at ~2s
		// target duration + N workers can briefly outpace the
		// bridge's drain. The bridge collapses multiple hls
		// events into a rate-limited stream on the way out.
		//
		// startAttempt snapshots the emitter's cumulative
		// counters as the baseline for this hls.Run's deltas —
		// without it, hls's per-run counter reset would regress
		// the UI back to zero on every auth refresh.
		emitter.startAttempt()
		hlsProgress := make(chan hls.Progress, 32)
		go bridgeHLSProgress(emitter, hlsProgress)
		emitter.setStage("segments")

		// splitCtx lets OnWindowRoll cancel hls.Run independently
		// of parent ctx — distinguishes a forced split from a
		// user shutdown. thresholdSplitFired marks whether THIS
		// attempt's callback fired the cancel; PendingSplit can't
		// serve here (could be true at entry from a prior
		// attempt) and the err can't (orchestrator filters
		// context.Canceled to nil).
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

		// Shared range-gap path for first-poll and mid-stream window rolls.
		// Recording the lost range advances the durable frontier; threshold
		// accounting may then seal if buffered commits become contiguous.
		//
		// split is only used for first-poll resume rolls, where a large restart
		// gap can re-anchor the next part before threshold sealing/checkpointing.
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

		result, err := hls.Run(splitCtx, hls.JobConfig{
			MediaPlaylistURL:   variant.URL,
			WorkDir:            segmentsDir,
			Fetcher:            s.fetcher,
			SegmentConcurrency: s.cfg.App.Download.SegmentConcurrency,
			Log:                log,
			Progress:           hlsProgress,
			StartMediaSeq:      startSeq,
			ClassifyAuth:       classifyTwitchAuth,
			// Per-part gap policy: seed the new attempt's
			// counters with the cumulative totals so the first-
			// content-segment guard and MaxGapRatio evaluate
			// against the whole part, not just this attempt.
			// Auth refresh mid-stream can't erase "real content
			// already captured" or reset the ratio denominator.
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
					// Size/duration part split: fold this committed
					// segment's bytes + EXTINF into the durable
					// per-part totals only when it reaches the
					// contiguous frontier, then cut at the first
					// frontier sequence that reaches the ceiling.
					// Above-boundary in-flight results are ignored/
					// pruned and refetched by the next part, which
					// preserves the no gap/no duplicate invariant
					// under concurrent workers.
					acct.commit(ev.MediaSeq, ev.BytesWritten, ev.DurationSeconds)
				case hls.OutcomeGapAccepted:
					acct.gap(ev.MediaSeq, GapReasonFetchFailure)
				case hls.OutcomeAdSkipped:
					acct.gap(ev.MediaSeq, GapReasonStitchedAd)
				case hls.OutcomeMalformedSkip:
					// Structural manifest defect — distinct from
					// fetch failures so operator review can see
					// whether the loss was transport or metadata.
					acct.gap(ev.MediaSeq, GapReasonMalformed)
				case hls.OutcomeAuth:
					// Auth-errored seqs are gapped from the
					// current attempt's perspective — the next
					// auth-refresh attempt's StartMediaSeq skips
					// past via LastMediaSeq+1. Recording as a
					// resume gap preserves that decision across
					// a crash-restart within the refresh window.
					// Ceiling-free on purpose (see authGap).
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
		// Translate a scoped-context cancel from either in-attempt
		// split callback into its sentinel so the outer loop treats it
		// as a split. Both callbacks checkpointed PendingSplit before
		// cancelSplit, so a parent-ctx cancel (shutdown) racing ahead
		// still resumes down the right path — mapForcedSplitErr leaves
		// err untouched in that case.
		// A restart-gap split has no sealed boundary; a threshold split
		// always sealed one (maybeForcePartThreshold seals before setting
		// partThresholdFired), so PendingSplitBoundarySet is the durable
		// proof.
		err = mapForcedSplitErr(ctx, err, thresholdSplitFired, false, ErrRestartGapExceeded, "forced part split at restart gap")
		err = mapForcedSplitErr(ctx, err, partThresholdFired, d.resume.PendingSplitBoundarySet, ErrPartThresholdExceeded,
			fmt.Sprintf("part %d reached size/duration ceiling", d.resume.CurrentPartIndex))
		// Unconditional checkpoint between attempts — captures
		// any trailing events from the batch counter and the
		// latest stage info before the next refresh iteration.
		s.checkpointResume(dbCtx, d, log)
		eventsSinceCheckpoint = 0

		// Refetch list is rebuilt below, after the fold updates the
		// unresolved-canceled set. Replace (don't append): a successful
		// refetch drops the seq off, a repeat failure shows up again.
		refetchSeqs = nil

		// Fold this attempt's counters into the running total.
		// Done/Gaps are SEEDED into each hls.Run (per-part gap
		// policy), so result already carries the cumulative
		// totals — overwrite instead of accumulating to avoid
		// double counting. AdGaps and BytesWritten are NOT
		// seeded, so `+=` remains correct for them.
		//
		// Kind + InitURI come from whichever attempt most
		// recently had them set — the manifest side shouldn't
		// flip between attempts for the same variant, but if
		// it does the final value wins.
		if result != nil {
			foldHLSAttemptResult(agg, result, d.resume, unresolvedCanceled)
			// Auth-errored seqs (fresh token) PLUS any still-unresolved
			// canceled in-flight fetches — the latter sit below the
			// advanced startSeq and would otherwise never be re-emitted.
			refetchSeqs = refetchSeqsForNextAttempt(result.AuthErrorSeqs, unresolvedCanceled)
			startSeq = agg.LastMediaSeq + 1
		}

		if err == nil {
			return agg, nil
		}
		if !errors.Is(err, hls.ErrPlaylistAuth) {
			// Gap abort, transport exhaustion on the
			// playlist, ctx cancel — not fixable by refresh.
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

// refetchSeqsForNextAttempt unions the auth-errored seqs (which need a
// fresh playback token) with the still-unresolved canceled seqs
// (in-flight fetches the orchestrator dropped when the run was canceled,
// left below the advanced startSeq). Both must be re-emitted on the next
// attempt: a canceled seq carries through orchestrator.SegmentsCanceled
// but is neither an auth seq nor >= startSeq, so without re-listing it as
// a refetch the poller never re-emits it — a permanent hole below
// LastMediaSeq that stalls the frontier and pins EndList false. Sorted
// for a deterministic refetch order.
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
	// ENDLIST is sticky across attempts only after every previously
	// canceled same-sequence retry has been durably resolved. Without
	// this, an auth-refresh attempt can skip over an ignored canceled
	// final segment and later mark the aggregate complete.
	if result.EndList && len(unresolvedCanceled) == 0 {
		agg.EndList = true
	}
}

// resolveVariantURL walks Stages 1-3 and returns the freshly-
// selected variant — URL plus quality + codec metadata the
// progress emitter surfaces to the UI.
//
// When a service account is configured, the playback-token GQL
// call carries Authorization: OAuth <access_token> — unlocks
// ad-free playback on Turbo accounts and HEVC variants on
// channels whose transcode ladder serves HEVC to authenticated
// viewers. A refresh failure or unset refresh token falls back
// to anonymous playback rather than failing the job.
func (s *Service) resolveVariantURL(ctx context.Context, p Params, opts twitch.SelectOptions) (twitch.SelectedVariant, error) {
	accessToken := s.svcAcct.Token(ctx)
	token, err := s.twitch.PlaybackToken(ctx, p.BroadcasterLogin, accessToken)
	if err != nil {
		return twitch.SelectedVariant{}, fmt.Errorf("playback token: %w", err)
	}
	manifest, err := s.twitch.FetchMasterPlaylist(ctx, p.BroadcasterLogin, token, opts)
	if err != nil {
		return twitch.SelectedVariant{}, fmt.Errorf("master playlist: %w", err)
	}
	variant, err := twitch.SelectVariant(manifest, opts)
	if err != nil {
		return twitch.SelectedVariant{}, fmt.Errorf("variant selection: %w", err)
	}
	return variant, nil
}

// kindFromRecordingType maps the spec's recording_type enum to
// remux.Kind. Empty or unknown values fall through to video,
// matching the repo CHECK constraint's default.
func kindFromRecordingType(rt string) remux.Kind {
	if rt == twitch.RecordingTypeAudio {
		return remux.KindAudio
	}
	return remux.KindVideo
}

// isCorrupt applies the spec's Stage 9 duration-mismatch rule.
// Zero durations on either side are treated as "can't measure,
// don't heal" — probe.parseProbeOutput returns zero on "N/A"
// values and we'd rather skip healing than trigger it on noise.
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

// bridgeHLSProgress forwards hls.Progress events into the
// downloader's progressEmitter until the hls channel is closed
// (which the hls orchestrator does on every termination path).
// The emitter handles the cumulative-state + speed-window math;
// this function just pumps the channel.
//
// fetchWithAuthRefresh may spawn a new bridgeHLSProgress per
// iteration. Two bridges may briefly coexist if a previous
// iteration's drain hasn't finished before the next hls.Run
// starts — both write to the same progressEmitter, which uses
// its own mutex, so concurrent writes are safe. Events stay
// cumulative so any interleaving still produces coherent state
// at the subscriber.
func bridgeHLSProgress(emitter *progressEmitter, in <-chan hls.Progress) {
	for hp := range in {
		emitter.bridge(hp)
	}
}

// uploadFromScratch opens a scratch file and streams it to the
// Storage backend at the given relative path. For local storage
// this is an atomic move; for S3 it uploads bytes.
func (s *Service) uploadFromScratch(ctx context.Context, scratchPath, storagePath string) error {
	f, err := os.Open(scratchPath)
	if err != nil {
		return fmt.Errorf("open scratch: %w", err)
	}
	defer f.Close()
	if err := s.storage.Save(ctx, filepath.ToSlash(storagePath), f); err != nil {
		return fmt.Errorf("save to storage: %w", err)
	}
	return nil
}

// setResumeStage latches the next pipeline stage on the in-memory
// checkpoint and persists the whole state to jobs.resume_state.
// Called at every stage boundary in run(); a crash-restart reads
// the row to decide where to pick up.
//
// Uses dbCtx (context.WithoutCancel of the run ctx) so a user
// Cancel() still lets the final checkpoint write land. Errors
// are logged and swallowed: a failed checkpoint doesn't derail
// the pipeline — the worst case is resume kicks in at a coarser
// stage and re-runs idempotent work.
func (s *Service) setResumeStage(dbCtx context.Context, d *download, stage Stage, log *slog.Logger) {
	d.resume.SetStage(stage)
	s.checkpointResume(dbCtx, d, log)
}

// checkpointResume persists the current in-memory ResumeState to
// jobs.resume_state without changing the stage. Used from the
// OnEvent batch path where segment outcomes have updated the
// frontier but the stage hasn't transitioned.
func (s *Service) checkpointResume(dbCtx context.Context, d *download, log *slog.Logger) {
	data, err := json.Marshal(d.resume)
	if err != nil {
		log.Error("resume state marshal failed", "error", err, "stage", d.resume.Stage)
		return
	}
	if err := s.repo.UpdateJobResumeState(dbCtx, d.jobID, data); err != nil {
		log.Error("resume state persist failed", "error", err, "stage", d.resume.Stage)
	}
}

// failDownload records a failure on the video row. If the
// download was cancelled by a user call to Cancel(), the
// recorded error is ErrCancelled so the UI can distinguish
// "admin stopped this" from a real crash.
//
// Shutdown case: when s.shuttingDown is set AND the user did NOT
// cancel, we flush the final resume-state checkpoint but do NOT
// mark video/job as FAILED — the row stays RUNNING for Resume()
// to pick up on next boot. Spec line 625 "Shutdown is not a
// download failure."
func (s *Service) failDownload(dbCtx context.Context, d *download, log *slog.Logger, cause error) {
	s.mu.Lock()
	userCancelled := d.userCancelled
	s.mu.Unlock()

	if s.shuttingDown.Load() && !userCancelled {
		if err := s.repo.CloseOpenVideoMetadataSpans(dbCtx, d.videoID, time.Now().UTC()); err != nil {
			log.Warn("close video metadata spans on shutdown", "video_id", d.videoID, "error", err)
		}
		// Job stays RUNNING for next boot's Resume. Do NOT set
		// cleanupScratch — segments on disk are what Resume
		// needs to pick up from PrepareInput without re-
		// downloading. sweepOrphanedTempsExcept at next boot
		// preserves this dir via the active-RUNNING protected
		// set.
		log.Info("download interrupted by shutdown; leaving RUNNING for resume",
			"error", cause,
			"stage", d.resume.Stage,
			"accounted_frontier", d.resume.AccountedFrontierMediaSeq)
		s.checkpointResume(dbCtx, d, log)
		return
	}

	recorded := cause
	if err := s.repo.CloseOpenVideoMetadataSpans(dbCtx, d.videoID, time.Now().UTC()); err != nil {
		log.Warn("close video metadata spans on failure", "video_id", d.videoID, "error", err)
	}
	if userCancelled {
		recorded = ErrCancelled
		log.Info("download cancelled by user")
	} else {
		log.Error("download failed", "error", cause)
	}
	// completion_kind for terminal failures, in priority order:
	//
	//   cancelled — operator stopped the run via Cancel(). UI shows
	//               a grey CANCELLED badge instead of red FAILED.
	//   partial   — at least one part has been remuxed and persisted
	//               (size_bytes > 0 in video_parts). The recording
	//               file exists and is watchable; the run failed
	//               before the next part finished. Surfacing this
	//               distinguishes "we have something for you" from
	//               "this run produced nothing recoverable" in the
	//               videos page Partial tab.
	//   complete  — fallthrough for failed runs that never finalized
	//               a part (auth failure pre-segments, immediate
	//               playlist 404, fetch retries exhausted before any
	//               part rolled over). FAILED with no salvage. UI
	//               reads the error field for details.
	//
	// HasFinalizedVideoParts is one cheap EXISTS-on-index query —
	// the failure path is rare enough that an extra round-trip is
	// fine. On its own error we keep the existing safe default
	// (complete) and log; better than mis-stamping a partial label
	// because of a transient repo glitch.
	failCompletionKind := repository.CompletionKindComplete
	switch {
	case userCancelled:
		failCompletionKind = repository.CompletionKindCancelled
	default:
		// dbCtx is context.WithoutCancel(parentCtx) at the top of run()
		// so a canceled parent doesn't bleed into terminal writes —
		// any error here is a real repo failure, not the run's own
		// cancellation. Safe-default to complete on error rather than
		// risk mis-stamping partial.
		hasPart, err := s.repo.HasFinalizedVideoParts(dbCtx, d.videoID)
		if err != nil {
			log.Warn("classify failure: check finalized parts", "video_id", d.videoID, "error", err)
		} else if hasPart {
			failCompletionKind = repository.CompletionKindPartial
		}
	}
	// truncated for FAILED: same axes as the success path. Cancelled
	// runs imply truncated (operator stopped a live recording).
	// HadWindowRoll implies truncated (CDN advanced past us, broadcast
	// kept going). EndListSeen=false implies truncated (playlist never
	// closed, recorder ended early). A REMUX/STORE failure after
	// EndListSeen=true is a *post-broadcast* failure — the artifact
	// wasn't produced, but the recording wasn't cut short relative to
	// the broadcast.
	truncated := userCancelled || d.resume.HadWindowRoll || !d.resume.EndListSeen
	delivery := s.recordingWebhookDelivery(d.videoID, recordingwebhook.EventFailed)
	if err := s.repo.MarkVideoFailedAndEnqueueRecordingWebhook(dbCtx, d.videoID, recorded.Error(), failCompletionKind, truncated, delivery); err != nil {
		log.Error("failed to mark video failed", "error", err)
	}
	if err := s.repo.MarkJobFailed(dbCtx, d.jobID, recorded.Error()); err != nil {
		log.Error("failed to mark job failed", "error", err)
	}
	// Terminal-for-this-attempt outcome: the job is now FAILED
	// (user cancel or real failure). FAILED rows are excluded from
	// Resume's RUNNING/PENDING query, so keeping scratch would just
	// leak until next boot's sweep. Wipe now.
	d.cleanupScratch = true
	// Wake the webhook dispatcher. Only real failures and cancels reach here —
	// the shutdown branch returned early above, so an interrupted recording that
	// stays RUNNING for resume never queues or wakes a webhook.
	s.publishRecordingTerminal(d.videoID, eventbus.RecordingFailed)
}

// classifyTwitchAuth wires the twitch-specific entitlement-code
// classifier into the hls package's generic ClassifyAuth hook.
// Returns true when the body carries a permanent code
// (subscriber-only, geoblock, VOD-manifest-restricted) so the
// caller can fail fast instead of refreshing. False for any other
// 401/403 — a stale signed URL that a fresh token will fix.
//
// hls package is intentionally Twitch-agnostic; binding this on
// the downloader side keeps the classifier one function call off
// the hot path without leaking Twitch symbols into hls/.
func classifyTwitchAuth(status int, body []byte) bool {
	return twitch.IsPermanent(twitch.NewAuthError(status, body))
}

// storageSnapshotWriter adapts storage.Storage to the
// thumbnail.SnapshotWriter interface. Writes each capture to the
// deterministic key storagekeys.Snapshot builds:
//
//	thumbnails/<filename>-snap00.jpg
//	thumbnails/<filename>-snap01.jpg
//	...
//
// The UI and retention discover the set by probing those keys (via
// storagekeys.Snapshot) until the first gap.
//
// ctx is the recording's long-lived context (NOT the snapshotter's
// derived ctx). An upload that starts right before the snapshotter
// ctx cancels should still be allowed to finish — otherwise a tick
// firing at the same moment as "recording done" would be lost.
// The outer run() ctx + user cancel still tear everything down if
// the whole job is canceled.
type storageSnapshotWriter struct {
	storage              storage.Storage
	filename             string
	ctx                  context.Context
	onFirstSnapshotSaved func(path string)
}

func (w *storageSnapshotWriter) WriteSnapshot(_ context.Context, index int, body io.Reader) error {
	path := storagekeys.Snapshot(w.filename, index)
	if err := w.storage.Save(w.ctx, path, body); err != nil {
		return err
	}
	if index == 0 && w.onFirstSnapshotSaved != nil {
		w.onFirstSnapshotSaved(path)
	}
	return nil
}

// buildFilename generates a deterministic, filesystem-safe
// filename tied to the job ID so a retry of the same broadcaster
// doesn't clobber the original. Format:
// <UTC timestamp>-<login>-<short jobID>.
func buildFilename(login, jobID string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	short := strings.ReplaceAll(jobID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("%s-%s-%s", ts, login, short)
}
