package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type downloadRepo interface {
	GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error)
	GetVideoByJobID(ctx context.Context, jobID string) (*repository.Video, error)
}

type downloadRunner interface {
	Start(ctx context.Context, p downloader.Params) (string, error)
	Cancel(jobID string)
	Subscribe(jobID string) <-chan downloader.Progress
	ListActiveProgress() []downloader.Progress
	SubscribeActive(ctx context.Context) <-chan struct{}
}

type streamHydrator interface {
	Hydrate(ctx context.Context, broadcasterID string) *streammeta.Snapshot
}

// ErrChannelNotSynced is returned by DownloadService.Trigger when the
// broadcaster has no channels row. The transport layer maps this to
// 404 with a pointer to channel.syncFromTwitch so the operator knows
// the fix.
var ErrChannelNotSynced = errors.New("video: channel not synced")

// DownloadService owns the download control-plane business logic:
// trigger (validate channel exists, attach user-token context for
// Helix attribution, enqueue into the downloader), cancel, and
// progress subscription.
//
// The actual download/ffmpeg pipeline lives in internal/downloader.
// This service is the operator-facing wrapper that sits between the
// tRPC transport and that pipeline.
type DownloadService struct {
	repo       downloadRepo
	downloader downloadRunner
	twitch     *twitch.Client
	hydrator   streamHydrator
	log        *slog.Logger
}

// NewDownload builds the download control-plane service. hydrator is
// shared with the schedule processor so manual + auto triggers both
// upsert the same streams / categories / tags / titles rows — and
// therefore the manual path fills videos.stream_id safely, since the
// FK-parent row is guaranteed to exist by the time CreateVideo runs.
func NewDownload(repo repository.Repository, dl *downloader.Service, tc *twitch.Client, hydrator *streammeta.Hydrator, log *slog.Logger) *DownloadService {
	return &DownloadService{
		repo:       repo,
		downloader: dl,
		twitch:     tc,
		hydrator:   hydrator,
		log:        log.With("domain", "download"),
	}
}

// TriggerInput carries everything needed to queue a manual download
// from a tRPC procedure: the broadcaster ID, requested quality, the
// recording mode + codec preference, and the caller's identity so
// Helix calls + fetch logs attribute to them rather than the app
// credential.
//
// RecordingType + ForceH264 are persisted on the videos row AND
// forwarded to the downloader so the native pipeline's Stage 3
// variant selector picks the right rendition (audio_only vs the
// quality chain) and applies the codec filter. Operator intent
// survives across restarts (videos row) and affects the in-flight
// pipeline (Params).
type TriggerInput struct {
	BroadcasterID string
	RecordingType string
	Quality       string
	ForceH264     bool
	UserID        string
}

// TriggerResult is what Trigger hands back. JobID is always set;
// VideoID may be zero when the video row was queued but the reload
// read failed (rare — logged, caller should still surface JobID).
type TriggerResult struct {
	JobID   string
	VideoID int64
}

// Trigger validates the channel exists, attaches user identity to
// the download context, and kicks off the pipeline. Returns
// ErrChannelNotSynced if the broadcaster has no channels row —
// admins must run channel.syncFromTwitch first so the video row's
// FK is satisfied.
func (s *DownloadService) Trigger(ctx context.Context, input TriggerInput) (TriggerResult, error) {
	quality := input.Quality
	if quality == "" {
		quality = repository.QualityHigh
	}

	ch, err := s.repo.GetChannel(ctx, input.BroadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TriggerResult{}, ErrChannelNotSynced
		}
		return TriggerResult{}, fmt.Errorf("get channel: %w", err)
	}

	// Attach user identity so Helix calls from the download flow are
	// attributed correctly and fetch logs carry the right user.
	downloadCtx := twitch.WithUserID(ctx, input.UserID)

	// Hydrate from Helix via the shared service: persists streams +
	// categories + tags + titles rows and returns a snapshot for the
	// Video row's metadata. Best-effort — any Helix failure yields a
	// nil snapshot and we proceed with empty title / no stream link.
	// Same contract as the schedule-processor path, so manual and
	// auto triggers leave the DB in the same shape.
	var (
		title        string
		viewers      int64
		streamID     *string
		language     string
		categoryID   string
		categoryName string
	)
	if s.hydrator != nil {
		if snap := s.hydrator.Hydrate(downloadCtx, ch.BroadcasterID); snap != nil {
			title = snap.Title
			viewers = snap.ViewerCount
			language = snap.Language
			categoryID = snap.GameID
			categoryName = snap.GameName
			if snap.StreamID != "" {
				// streams row was upserted — safe to set the FK.
				id := snap.StreamID
				streamID = &id
			}
		}
	}
	// Language falls back to the channel's default only when Helix
	// didn't give us a per-stream language.
	if language == "" {
		language = derefString(ch.BroadcasterLanguage)
	}

	jobID, err := s.downloader.Start(downloadCtx, downloader.Params{
		BroadcasterID:    ch.BroadcasterID,
		BroadcasterLogin: ch.BroadcasterLogin,
		DisplayName:      ch.BroadcasterName,
		Title:            title,
		CategoryID:       categoryID,
		CategoryName:     categoryName,
		Quality:          quality,
		Language:         language,
		ViewerCount:      viewers,
		StreamID:         streamID,
		RecordingType:    input.RecordingType,
		ForceH264:        input.ForceH264,
	})
	if err != nil {
		return TriggerResult{}, fmt.Errorf("start download: %w", err)
	}

	v, err := s.repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		// Download is already queued; the video row may land a tick
		// behind the jobID hand-back on slow DBs. Log + surface the
		// jobID so the dashboard can still subscribe to progress.
		s.log.Error("reload video after start", "error", err, "job_id", jobID)
		return TriggerResult{JobID: jobID}, nil
	}
	return TriggerResult{JobID: jobID, VideoID: v.ID}, nil
}

// Cancel asks the downloader to terminate an active job. No-op when
// the job has already finished or never existed — the downloader
// handles that internally; we don't need an error channel for it.
func (s *DownloadService) Cancel(_ context.Context, jobID string) {
	s.downloader.Cancel(jobID)
}

// Subscribe returns the progress channel for an active job. Returns
// nil when the job is not (or no longer) active; the transport layer
// turns nil into a pre-closed SSE stream so clients see a clean
// completion rather than a hang or error.
func (s *DownloadService) Subscribe(jobID string) <-chan downloader.Progress {
	return s.downloader.Subscribe(jobID)
}

func (s *DownloadService) ActiveProgress() []downloader.Progress {
	return s.downloader.ListActiveProgress()
}

// VideoByJobID exposes the downloader-service's video lookup so the
// handler can resolve the active-downloads set from in-memory job
// IDs instead of scanning a page of RUNNING rows.
func (s *DownloadService) VideoByJobID(ctx context.Context, jobID string) (*repository.Video, error) {
	return s.repo.GetVideoByJobID(ctx, jobID)
}

func (s *DownloadService) SubscribeActive(ctx context.Context) <-chan struct{} {
	return s.downloader.SubscribeActive(ctx)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
