package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type downloadRepo interface {
	GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error)
	GetVideoByJobID(ctx context.Context, jobID string) (*repository.Video, error)
	ListVideosByJobIDs(ctx context.Context, jobIDs []string) ([]repository.Video, error)
}

type downloadRunner interface {
	Start(ctx context.Context, p downloader.Params) (string, error)
	Cancel(jobID string) error
	Subscribe(jobID string) <-chan downloader.Progress
	ListActiveProgress() []downloader.Progress
	SubscribeActive(ctx context.Context) <-chan struct{}
	LiveRenditions(ctx context.Context, login string, forceH264 bool) (downloader.LiveRenditions, error)
}

type streamHydrator interface {
	Hydrate(ctx context.Context, broadcasterID string) *streammeta.Snapshot
}

// ErrChannelNotSynced means Trigger or LiveRenditions needs a locally synced channel.
var ErrChannelNotSynced = errors.New("video: channel not synced")

type DownloadService struct {
	repo                 downloadRepo
	downloader           downloadRunner
	twitch               *twitch.Client
	hydrator             streamHydrator
	maxConcurrent        int
	archiveMaxConcurrent int
	log                  *slog.Logger
}

func (s *DownloadService) MaxConcurrent() int {
	return s.maxConcurrent
}

func (s *DownloadService) ArchiveMaxConcurrent() int {
	return s.archiveMaxConcurrent
}

// NewDownload shares hydrator with scheduled recordings so both paths use the
// same metadata cache.
func NewDownload(repo repository.Repository, dl *downloader.Service, tc *twitch.Client, hydrator *streammeta.Hydrator, log *slog.Logger) *DownloadService {
	maxConcurrent, archiveMaxConcurrent := 0, 0
	if dl != nil {
		maxConcurrent = dl.MaxConcurrent()
		archiveMaxConcurrent = dl.ArchiveMaxConcurrent()
	}
	return &DownloadService{
		repo:                 repo,
		downloader:           dl,
		twitch:               tc,
		hydrator:             hydrator,
		maxConcurrent:        maxConcurrent,
		archiveMaxConcurrent: archiveMaxConcurrent,
		log:                  log.With("domain", "download"),
	}
}

type TriggerInput struct {
	BroadcasterID string
	RecordingType string
	Quality       string
	ForceH264     bool
	// MaxHeight pins the recording to an exact rendition height from
	// LiveRenditions; it wins over Quality and is ignored for audio.
	MaxHeight int
	UserID    string
}

// TriggerResult always includes JobID on success. VideoID is zero if admission
// committed but reloading the video failed; callers can still follow JobID.
type TriggerResult struct {
	JobID   string
	VideoID int64
}

// Trigger starts a manual recording and returns ErrChannelNotSynced when its
// broadcaster has no local channel row.
func (s *DownloadService) Trigger(ctx context.Context, input TriggerInput) (TriggerResult, error) {
	// Store the tier containing a pinned video height to satisfy the quality CHECK
	// and library filters. Audio ignores the height.
	quality := input.Quality
	maxHeight := 0
	if repository.NormalizeRecordingType(input.RecordingType) == repository.RecordingTypeVideo && input.MaxHeight > 0 {
		maxHeight = input.MaxHeight
		quality = repository.QualityTierForHeight(maxHeight)
	} else if quality == "" {
		quality = repository.QualityHigh
	}
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: input.RecordingType,
		Quality:       quality,
		ForceH264:     input.ForceH264,
	})

	ch, err := s.repo.GetChannel(ctx, input.BroadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TriggerResult{}, ErrChannelNotSynced
		}
		return TriggerResult{}, fmt.Errorf("get channel: %w", err)
	}

	// Attribute Helix requests and fetch logs to the initiating user.
	downloadCtx := twitch.WithUserID(ctx, input.UserID)

	// Helix enrichment is best-effort; a failed lookup leaves metadata empty.
	var (
		streamStartedAt time.Time
		title           string
		viewers         int64
		streamID        *string
		language        string
		categoryID      string
		categoryName    string
	)
	if s.hydrator != nil {
		if snap := s.hydrator.Hydrate(downloadCtx, ch.BroadcasterID); snap != nil {
			streamStartedAt = snap.StartedAt
			title = snap.Title
			viewers = snap.ViewerCount
			language = snap.Language
			categoryID = snap.GameID
			categoryName = snap.GameName
			if snap.StreamID != "" {
				id := snap.StreamID
				streamID = &id
			}
		}
	}
	if language == "" {
		language = derefString(ch.BroadcasterLanguage)
	}

	jobID, err := s.downloader.Start(downloadCtx, downloader.Params{
		StreamStartedAt:  streamStartedAt,
		BroadcasterID:    ch.BroadcasterID,
		BroadcasterLogin: ch.BroadcasterLogin,
		DisplayName:      ch.BroadcasterName,
		Title:            title,
		CategoryID:       categoryID,
		CategoryName:     categoryName,
		Quality:          settings.Quality,
		Language:         language,
		ViewerCount:      viewers,
		StreamID:         streamID,
		RecordingType:    settings.RecordingType,
		ForceH264:        settings.ForceH264,
		MaxHeight:        maxHeight,
	})
	if err != nil {
		return TriggerResult{}, fmt.Errorf("start download: %w", err)
	}

	v, err := s.repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		// Admission already committed; return JobID so progress stays addressable
		// when this reload fails.
		s.log.Error("reload video after start", "error", err, "job_id", jobID)
		return TriggerResult{JobID: jobID}, nil
	}
	return TriggerResult{JobID: jobID, VideoID: v.ID}, nil
}

// Cancel returns the result of the downloader's durable stop request.
func (s *DownloadService) Cancel(_ context.Context, jobID string) error {
	return s.downloader.Cancel(jobID)
}

// Subscribe returns nil for inactive jobs; callers should close the progress
// stream in that case.
func (s *DownloadService) Subscribe(jobID string) <-chan downloader.Progress {
	return s.downloader.Subscribe(jobID)
}

func (s *DownloadService) ActiveProgress() []downloader.Progress {
	return s.downloader.ListActiveProgress()
}

// VideosByJobIDs resolves active jobs in one query, omitting missing rows.
// Use the downloader's job IDs; paging RUNNING rows can hide active work behind
// orphaned rows from a previous process.
func (s *DownloadService) VideosByJobIDs(ctx context.Context, jobIDs []string) ([]repository.Video, error) {
	return s.repo.ListVideosByJobIDs(ctx, jobIDs)
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

// LiveRenditions lists what a download started now could record for the
// channel, resolved through the recorder's playback session under the same
// codec choice the download would make.
func (s *DownloadService) LiveRenditions(ctx context.Context, broadcasterID string, forceH264 bool) (downloader.LiveRenditions, error) {
	ch, err := s.repo.GetChannel(ctx, broadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return downloader.LiveRenditions{}, ErrChannelNotSynced
		}
		return downloader.LiveRenditions{}, fmt.Errorf("get channel: %w", err)
	}
	return s.downloader.LiveRenditions(ctx, ch.BroadcasterLogin, forceH264)
}
