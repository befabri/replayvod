// Package downloadservice owns the download control-plane business
// logic: trigger (validate channel exists, attach user-token context
// for Helix attribution, enqueue into the downloader), cancel, and
// progress subscription.
//
// The actual yt-dlp/ffmpeg pipeline lives in internal/downloader.
// This package is the operator-facing wrapper that sits between the
// tRPC transport and that pipeline.
package downloadservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// ErrChannelNotSynced is returned by Trigger when the broadcaster has
// no channels row. The transport layer maps this to 404 with a
// pointer to channel.syncFromTwitch so the operator knows the fix.
var ErrChannelNotSynced = errors.New("downloadservice: channel not synced")

type Service struct {
	repo       repository.Repository
	downloader *downloader.Service
	twitch     *twitch.Client
	log        *slog.Logger
}

func New(repo repository.Repository, dl *downloader.Service, tc *twitch.Client, log *slog.Logger) *Service {
	return &Service{
		repo:       repo,
		downloader: dl,
		twitch:     tc,
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
	BroadcasterID   string
	RecordingType   string
	Quality         string
	ForceH264       bool
	UserID          string
	UserAccessToken string
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
func (s *Service) Trigger(ctx context.Context, input TriggerInput) (TriggerResult, error) {
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
	downloadCtx := twitch.WithUserToken(ctx, input.UserAccessToken)
	downloadCtx = twitch.WithUserID(downloadCtx, input.UserID)

	jobID, err := s.downloader.Start(downloadCtx, downloader.Params{
		BroadcasterID:    ch.BroadcasterID,
		BroadcasterLogin: ch.BroadcasterLogin,
		DisplayName:      ch.BroadcasterName,
		Quality:          quality,
		Language:         derefString(ch.BroadcasterLanguage),
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
func (s *Service) Cancel(_ context.Context, jobID string) {
	s.downloader.Cancel(jobID)
}

// Subscribe returns the progress channel for an active job. Returns
// nil when the job is not (or no longer) active; the transport layer
// turns nil into a pre-closed SSE stream so clients see a clean
// completion rather than a hang or error.
func (s *Service) Subscribe(jobID string) <-chan downloader.Progress {
	return s.downloader.Subscribe(jobID)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
