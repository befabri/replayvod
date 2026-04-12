// Package downloader runs the native Go HLS → ffmpeg pipeline that
// turns a live Twitch stream into a stored MP4. Each download has
// a unique jobID so callers (tRPC handlers, the scheduler, webhook
// handlers) can subscribe to progress updates and request
// cancellation without holding a reference to the in-flight work.
//
// Pipeline composition (spec stages 1-11):
//
//	1. twitch.Client.PlaybackToken            — GQL access token
//	2. twitch.Client.FetchMasterPlaylist      — usher manifest
//	3. twitch.SelectVariant                   — quality/codec pick
//	4. hls.Run                                — segments → scratch
//	5. remux.PrepareInput                     — segments.txt / media.m3u8
//	6. remux.Remuxer.Run                      — ffmpeg → mp4/m4a
//	7. probe.Probe.Run                        — duration + streams
//	8. thumbnail.Generator.Generate           — jpg at 10% (video only)
//	9. corruption check → remux.Remuxer.Heal  — if duration drifts >50s
//	10. storage.Save                          — upload to backend
//	11. os.RemoveAll(work_dir)                — cleanup
//
// Phase 6a scope: single-part happy path. Auth refresh (ErrPlaylistAuth
// → re-stages), resume-on-restart, part-splitting on variant/codec/
// container change, and stitched-ad gap segregation are Phase 6b+
// concerns.
package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/downloader/probe"
	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/downloader/thumbnail"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
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
	Quality          string
	Language         string
	ViewerCount      int64
	StreamID         *string

	// RecordingType is "video" (default) or "audio". Audio jobs
	// pick the audio_only rendition at Stage 3 and produce an
	// .m4a output. Empty defaults to video at the repo layer.
	RecordingType string

	// ForceH264 drops HEVC/AV1 variants before the Stage 3
	// quality-fallback chain. Operator-exposed per the spec's
	// "User codec preference" section.
	ForceH264 bool
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
	// (variant/codec/container switch). Phase 6d always emits 1
	// — part-splitting lands in a later phase.
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
	Quality string `json:"quality"`
	Codec   string `json:"codec"`

	// RecordingType mirrors the video row — "video" or "audio".
	RecordingType string `json:"recording_type"`
}

// Service orchestrates downloads. Safe for concurrent use. One
// Service per process; the pipeline components are constructed
// once in NewService and shared across all jobs.
type Service struct {
	cfg     *config.Config
	repo    repository.Repository
	storage storage.Storage
	log     *slog.Logger

	twitch  *twitch.Client
	fetcher *hls.Fetcher
	remuxer *remux.Remuxer
	probe   *probe.Probe
	thumb   *thumbnail.Generator
	svcAcct *serviceAccount

	mu     sync.Mutex
	active map[string]*download
}

// download is the per-job state kept in memory. cancel propagates
// a user Cancel() to every stage (playlist, fetch, remux, probe,
// thumbnail) via one shared ctx.
type download struct {
	jobID         string
	videoID       int64
	broadcasterID string
	cancel        context.CancelFunc
	userCancelled bool
	progressCh    chan Progress
	startedAt     time.Time

	// resume is the durable per-job checkpoint. Lives in memory
	// alongside the running pipeline; persisted to
	// jobs.resume_state on every material state transition so a
	// crash-restart can pick up without reprocessing completed
	// work. Zero-valued state (Stage=AUTH) is the "fresh job"
	// shape and is safe to persist as-is.
	resume *ResumeState

	// videoPartID is the row ID of the video_parts entry created
	// at Stage 5 (PrepareInput) and finalized at Stage 10 (Store).
	// Zero until CreateVideoPart succeeds. Phase 6g ships single-
	// part recordings; 6f grows this to an []int64 or similar.
	videoPartID int64
}

// NewService wires up the pipeline components. The twitch client,
// fetcher, remuxer, probe, and thumbnail generator are all
// process-lifetime singletons — they hold no per-job state.
func NewService(cfg *config.Config, repo repository.Repository, store storage.Storage, log *slog.Logger) *Service {
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
	}, domainLog)

	s := &Service{
		cfg:     cfg,
		repo:    repo,
		storage: store,
		log:     domainLog,
		twitch:  tw,
		fetcher: fetcher,
		remuxer: &remux.Remuxer{Log: domainLog},
		probe:   &probe.Probe{Log: domainLog},
		thumb:   &thumbnail.Generator{Log: domainLog},
		svcAcct: newServiceAccount(cfg.Env.ServiceAccountOAuthToken, domainLog),
		active:  make(map[string]*download),
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
func (s *Service) Start(ctx context.Context, p Params) (string, error) {
	s.mu.Lock()
	for _, existing := range s.active {
		if existing.broadcasterID == p.BroadcasterID {
			s.mu.Unlock()
			return "", ErrBusy
		}
	}
	maxConcurrent := s.cfg.App.Download.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	if len(s.active) >= maxConcurrent {
		s.mu.Unlock()
		return "", fmt.Errorf("downloader: at max concurrent downloads (%d)", maxConcurrent)
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
	s.active[jobID] = d
	s.mu.Unlock()

	vid, err := s.repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         jobID,
		Filename:      filename,
		DisplayName:   p.DisplayName,
		Status:        repository.VideoStatusPending,
		Quality:       p.Quality,
		BroadcasterID: p.BroadcasterID,
		StreamID:      p.StreamID,
		ViewerCount:   p.ViewerCount,
		Language:      p.Language,
		RecordingType: p.RecordingType,
		ForceH264:     p.ForceH264,
	})
	if err != nil {
		s.mu.Lock()
		delete(s.active, jobID)
		s.mu.Unlock()
		return "", fmt.Errorf("create video row: %w", err)
	}
	d.videoID = vid.ID

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
		_ = s.repo.MarkVideoFailed(ctx, vid.ID, fmt.Sprintf("create job row: %v", err))
		return "", fmt.Errorf("create job row: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

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

// Shutdown cancels all active downloads. Called from the server's
// graceful-shutdown path.
func (s *Service) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.active {
		d.cancel()
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
			_ = s.repo.MarkVideoFailed(ctx, job.VideoID, errMsg)
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

	s.mu.Lock()
	maxConcurrent := s.cfg.App.Download.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	if len(s.active) >= maxConcurrent {
		s.mu.Unlock()
		return fmt.Errorf("at max concurrent downloads (%d); cannot resume", maxConcurrent)
	}
	s.active[job.ID] = d
	s.mu.Unlock()

	runCtx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	// vid.Filename is the deterministic base name chosen at
	// original Start(); reuse it so the remuxed path is stable
	// across restart.
	go s.run(runCtx, d, p, vid.Filename)
	return nil
}

// ErrBusy is returned by Start when a download for the broadcaster
// is already in flight. Callers that want to replace the running
// download should call Cancel first.
//
// ErrCancelled marks a download that was terminated by a user
// Cancel() rather than crashing. Distinguishing matters for the UI.
var (
	ErrBusy      = errors.New("downloader: broadcaster already has an active download")
	ErrCancelled = errors.New("downloader: cancelled by user")
)

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
	emitter := newProgressEmitter(d.jobID, recordingType, d.progressCh)

	// Per-job scratch layout:
	//   <scratch>/<jobID>/segments/   — .ts / .m4s fragments + init.mp4
	//   <scratch>/<jobID>/<base>.mp4  — remuxed output
	//   <scratch>/<jobID>/<base>.jpg  — thumbnail
	// One RemoveAll at the end gets everything.
	jobDir := filepath.Join(s.cfg.Env.ScratchDir, d.jobID)
	segmentsDir := filepath.Join(jobDir, "segments")
	if err := os.MkdirAll(segmentsDir, 0o755); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("create scratch dir: %w", err))
		return
	}
	defer func() { _ = os.RemoveAll(jobDir) }()

	// Stages 1-4 run inside an auth-refresh loop. On
	// hls.ErrPlaylistAuth we re-mint a playback token, re-
	// fetch the master playlist, re-select the variant, and
	// resume the segment fetch at the cursor from the failed
	// attempt. Loop bounded by cfg.Download.AuthRefreshAttempts
	// so a stream we're never going to be allowed to play
	// doesn't loop indefinitely.
	selectOpts := twitch.SelectOptions{
		RecordingType: recordingType,
		Quality:       qualityToHeight(p.Quality),
		EnableAV1:     s.cfg.App.Download.EnableAV1,
		DisableHEVC:   s.cfg.App.Download.DisableHEVC,
		ForceH264:     p.ForceH264,
	}

	// Stage-aware skip on resume: when the checkpoint was already
	// past Stage 4 in a prior attempt, the segment fetch is
	// already done (files on disk, Resume preserved the scratch
	// dir) and re-running Stages 1-4 would fail or race —
	// playback tokens have rolled, the live stream may have
	// ended, and the ENDLIST poll would race the fetch loop.
	// Synthesize a minimal JobResult from the checkpoint and
	// jump straight to PrepareInput.
	//
	// Fresh jobs and jobs resumed at SEGMENTS take the normal
	// fetch path; the frontier seeding inside
	// fetchWithAuthRefresh handles "pick up where we left off."
	var hlsResult *hls.JobResult
	if d.resume.Stage.AtOrAfter(StagePrepareInput) {
		log.Info("resume: skipping segment fetch, checkpoint past SEGMENTS",
			"stage", d.resume.Stage,
			"accounted_frontier", d.resume.AccountedFrontierMediaSeq,
			"segment_format", d.resume.SegmentFormat)
		hlsResult = &hls.JobResult{
			Kind:           hls.SegmentKind(d.resume.SegmentFormat),
			LastMediaSeq:   d.resume.AccountedFrontierMediaSeq,
			SegmentsDone:   int64(len(d.resume.CompletedAboveFrontier)) + d.resume.AccountedFrontierMediaSeq - d.resume.PartStartMediaSequence + 1,
			SegmentsAdGaps: 0, // aggregates not reconstructed from resume_state
			SegmentsGaps:   int64(len(d.resume.Gaps)),
		}
	} else {
		// Stages 1-4 all live behind SEGMENTS as far as resume
		// dispatch is concerned — plan line 199 treats AUTH/
		// PLAYLIST as "no durable work, restart from Stage 1"
		// and SEGMENTS as "use the accounted frontier." Coarse-
		// grained checkpoint here; per-segment frontier updates
		// fire through OnEvent from inside fetchWithAuthRefresh.
		s.setResumeStage(dbCtx, d, StageSegments, log)
		var err error
		hlsResult, err = s.fetchWithAuthRefresh(ctx, dbCtx, d, emitter, p, segmentsDir, selectOpts, log)
		if err != nil {
			s.failDownload(dbCtx, d, log, err)
			return
		}
		// Segment count is now authoritative — total = done +
		// gaps. Fires one event so the UI transitions out of
		// the "-1 = unknown total" indeterminate-bar state
		// before remux begins.
		emitter.finalize()
	}

	// Stage 5: prepare ffmpeg input. Idempotent; a crash after
	// this but before REMUX just rebuilds the same segments.txt
	// / media.m3u8 on restart.
	kind := kindFromRecordingType(selectOpts.RecordingType)
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
	// probe runs. Phase 6g: always part_index=1. 6f grows this.
	//
	// On resume the row may already exist from the prior attempt;
	// look up by (video_id, part_index) first rather than
	// relying on CreateVideoPart to be idempotent at the adapter
	// layer (it isn't — DB unique constraint would fail).
	if existing, err := s.repo.GetVideoPartByIndex(dbCtx, d.videoID, d.resume.CurrentPartIndex); err == nil && existing != nil {
		d.videoPartID = existing.ID
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		s.failDownload(dbCtx, d, log, fmt.Errorf("lookup video part: %w", err))
		return
	} else {
		part, err := s.repo.CreateVideoPart(dbCtx, &repository.VideoPartInput{
			VideoID:       d.videoID,
			PartIndex:     d.resume.CurrentPartIndex,
			Filename:      filename + kind.OutputExt(),
			Quality:       d.resume.SelectedQuality,
			Codec:         d.resume.SelectedCodec,
			SegmentFormat: d.resume.SegmentFormat,
			StartMediaSeq: d.resume.PartStartMediaSequence,
		})
		if err != nil {
			s.failDownload(dbCtx, d, log, fmt.Errorf("create video part: %w", err))
			return
		}
		d.videoPartID = part.ID
	}

	s.setResumeStage(dbCtx, d, StagePrepareInput, log)
	emitter.setStage("remux")
	inputPath, err := remux.PrepareInput(segmentsDir, remuxMode)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("remux prep: %w", err))
		return
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
		OutputBasename: filename,
	}
	if err := s.remuxer.Run(ctx, remuxIn); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("remux: %w", err))
		return
	}
	remuxedPath := remuxIn.OutputPath()

	// Stage 7: probe.
	s.setResumeStage(dbCtx, d, StageProbe, log)
	emitter.setStage("metadata")
	probeResult, err := s.probe.Run(ctx, remuxedPath)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("probe: %w", err))
		return
	}

	// Stage 9: corruption check + heal. If duration mismatch is
	// within tolerance we skip entirely. On heal failure we keep
	// the un-healed file per spec ("partial VOD is better than
	// none").
	if isCorrupt(probeResult, kind) {
		s.setResumeStage(dbCtx, d, StageCorruptionCheck, log)
		log.Info("duration mismatch — running heal pass",
			"format_duration", probeResult.Duration,
			"threshold", remux.CorruptionThreshold)
		healedPath := filepath.Join(jobDir, filename+".healed"+kind.OutputExt())
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

	// Stage 8: thumbnail. Audio jobs skip entirely; the UI falls
	// back to the channel avatar.
	var thumbRel string
	if kind == remux.KindVideo {
		s.setResumeStage(dbCtx, d, StageThumbnail, log)
		emitter.setStage("thumbnail")
		thumbPath := filepath.Join(jobDir, filename+".jpg")
		err := s.thumb.Generate(ctx, thumbnail.Input{
			VideoPath:       remuxedPath,
			OutputPath:      thumbPath,
			DurationSeconds: probeResult.Duration,
		})
		switch {
		case err == nil:
			thumbRel = filepath.ToSlash(filepath.Join("thumbnails", filename+".jpg"))
		case errors.Is(err, thumbnail.ErrAllTriesSingleColor):
			log.Info("thumbnail: all frames monochrome; leaving unset")
		default:
			log.Warn("thumbnail generation failed; continuing without thumbnail", "error", err)
		}
	}

	// Stage 10: store. Video first, then thumbnail — if the
	// thumbnail upload fails we still want the video playable.
	s.setResumeStage(dbCtx, d, StageStore, log)
	videoRel := filepath.ToSlash(filepath.Join("videos", filename+kind.OutputExt()))
	if err := s.uploadFromScratch(ctx, remuxedPath, videoRel); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("upload video: %w", err))
		return
	}
	var thumbPtr *string
	if thumbRel != "" {
		thumbPath := filepath.Join(jobDir, filename+".jpg")
		if err := s.uploadFromScratch(ctx, thumbPath, thumbRel); err != nil {
			log.Warn("thumbnail upload failed; continuing without thumbnail", "error", err)
		} else {
			thumbPtr = &thumbRel
		}
	}

	// Stage 11: mark done. The deferred RemoveAll handles the
	// scratch cleanup.
	if err := s.repo.FinalizeVideoPart(dbCtx, &repository.VideoPartFinalize{
		ID:              d.videoPartID,
		DurationSeconds: probeResult.Duration,
		SizeBytes:       probeResult.Size,
		Thumbnail:       thumbPtr,
		EndMediaSeq:     hlsResult.LastMediaSeq,
	}); err != nil {
		log.Error("failed to finalize video part", "error", err)
		// Part row without finalization is a consistency smell
		// but videos.duration/size aggregate via SUM so one
		// unfinalized part doesn't hide the others. Continue to
		// the terminal marks rather than dragging the whole
		// pipeline down for a child-row update.
	}
	if err := s.repo.MarkVideoDone(dbCtx, d.videoID, probeResult.Duration, probeResult.Size, thumbPtr); err != nil {
		log.Error("failed to mark video done", "error", err)
		return
	}
	if err := s.repo.MarkJobDone(dbCtx, d.jobID); err != nil {
		log.Error("failed to mark job done", "error", err)
		// Job row stuck as RUNNING is a DB-consistency smell
		// but the video output is already committed and
		// uploaded — no value in surfacing this to the user.
	}
	emitter.setStage("done")
	log.Info("download complete",
		"duration_seconds", probeResult.Duration,
		"size_bytes", probeResult.Size,
		"gaps", hlsResult.SegmentsGaps,
		"segments", hlsResult.SegmentsDone,
	)
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

	// bootstrapped guards PartStartMediaSequence: first poll's
	// MediaSequenceBase anchors the frontier. Auth-refresh
	// iterations reuse the anchor — d.resume is shared across
	// attempts, so a refresh mid-stream doesn't reset the part.
	// A resumed job enters already bootstrapped from its prior
	// attempt's state; fresh jobs bootstrap on the first poll.
	bootstrapped := d.resume.PartStartMediaSequence != 0 || d.resume.AccountedFrontierMediaSeq != 0

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
		emitter.setStage("playlist")
		emitter.setVariant(variant.Quality, variant.Codec)
		// Mirror the selected variant into resume state so a
		// crash-restart between PREPARE_INPUT and STORE recovers
		// the exact (quality, codec) pair without re-walking
		// Stage 3. SegmentFormat lands after hls.Run returns —
		// it's a property of the media playlist, not the master.
		d.resume.SelectedQuality = variant.Quality
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

		result, err := hls.Run(ctx, hls.JobConfig{
			MediaPlaylistURL:   variant.URL,
			WorkDir:            segmentsDir,
			Fetcher:            s.fetcher,
			SegmentConcurrency: s.cfg.App.Download.SegmentConcurrency,
			Log:                log,
			Progress:           hlsProgress,
			StartMediaSeq:      startSeq,
			GapPolicy: hls.GapPolicy{
				Strict:      s.cfg.App.Download.Strict,
				MaxGapRatio: s.cfg.App.Download.MaxGapRatio,
			},
			OnFirstPoll: func(base int64) {
				if bootstrapped {
					return
				}
				bootstrapped = true
				d.resume.StartPart(base)
				s.checkpointResume(dbCtx, d, log)
			},
			OnWindowRoll: func(from, to int64) {
				// Resume gap: the CDN window rolled past the
				// frontier while we were down. Record the loss
				// so the frontier advances past it; without this
				// the frontier would stall forever waiting for
				// segments the edge no longer serves.
				d.resume.NoteRangeGap(from, to, GapReasonRestartWindowRolled)
				log.Warn("resume gap recorded",
					"reason", GapReasonRestartWindowRolled,
					"from", from,
					"to", to,
					"lost_segments", to-from+1)
				s.checkpointResume(dbCtx, d, log)
			},
			OnEvent: func(ev hls.SegmentEvent) {
				switch ev.Outcome {
				case hls.OutcomeCommitted:
					d.resume.NoteCommitted(ev.MediaSeq)
				case hls.OutcomeGapAccepted:
					d.resume.NoteGap(ev.MediaSeq, GapReasonFetchFailure)
				case hls.OutcomeAdSkipped:
					d.resume.NoteGap(ev.MediaSeq, GapReasonStitchedAd)
				case hls.OutcomeAuth:
					// Auth-errored seqs are gapped from the
					// current attempt's perspective — the next
					// auth-refresh attempt's StartMediaSeq skips
					// past via LastMediaSeq+1. Recording as a
					// resume gap preserves that decision across
					// a crash-restart within the refresh window.
					d.resume.NoteGap(ev.MediaSeq, GapReasonAuth)
				}
				eventsSinceCheckpoint++
				if eventsSinceCheckpoint >= checkpointEveryEvents {
					s.checkpointResume(dbCtx, d, log)
					eventsSinceCheckpoint = 0
				}
			},
		})
		// Unconditional checkpoint between attempts — captures
		// any trailing events from the batch counter and the
		// latest stage info before the next refresh iteration.
		s.checkpointResume(dbCtx, d, log)
		eventsSinceCheckpoint = 0

		// Fold this attempt's counters into the running total.
		// Kind + InitURI come from whichever attempt most
		// recently had them set — the manifest side shouldn't
		// flip between attempts for the same variant, but if
		// it does the final value wins.
		if result != nil {
			agg.SegmentsDone += result.SegmentsDone
			agg.SegmentsGaps += result.SegmentsGaps
			agg.SegmentsAdGaps += result.SegmentsAdGaps
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
func (s *Service) failDownload(dbCtx context.Context, d *download, log *slog.Logger, cause error) {
	s.mu.Lock()
	userCancelled := d.userCancelled
	s.mu.Unlock()

	recorded := cause
	if userCancelled {
		recorded = ErrCancelled
		log.Info("download cancelled by user")
	} else {
		log.Error("download failed", "error", cause)
	}
	if err := s.repo.MarkVideoFailed(dbCtx, d.videoID, recorded.Error()); err != nil {
		log.Error("failed to mark video failed", "error", err)
	}
	if err := s.repo.MarkJobFailed(dbCtx, d.jobID, recorded.Error()); err != nil {
		log.Error("failed to mark job failed", "error", err)
	}
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
