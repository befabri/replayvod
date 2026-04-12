package video

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC video procedures. Separate from the streaming
// Handler above — this serves JSON metadata, that serves bytes.
type Service struct {
	repo       repository.Repository
	downloader *downloader.Service
	twitch     *twitch.Client
	log        *slog.Logger
}

// NewService creates the video tRPC service.
func NewService(repo repository.Repository, dl *downloader.Service, tc *twitch.Client, log *slog.Logger) *Service {
	return &Service{
		repo:       repo,
		downloader: dl,
		twitch:     tc,
		log:        log.With("domain", "video"),
	}
}

// VideoResponse is the wire shape for a video record.
type VideoResponse struct {
	ID              int64      `json:"id"`
	JobID           string     `json:"job_id"`
	Filename        string     `json:"filename"`
	DisplayName     string     `json:"display_name"`
	Status          string     `json:"status"`
	Quality         string     `json:"quality"`
	BroadcasterID   string     `json:"broadcaster_id"`
	StreamID        *string    `json:"stream_id,omitempty"`
	ViewerCount     int64      `json:"viewer_count"`
	Language        string     `json:"language"`
	DurationSeconds *float64   `json:"duration_seconds,omitempty"`
	SizeBytes       *int64     `json:"size_bytes,omitempty"`
	Thumbnail       *string    `json:"thumbnail,omitempty"`
	Error           *string    `json:"error,omitempty"`
	StartDownloadAt time.Time  `json:"start_download_at"`
	DownloadedAt    *time.Time `json:"downloaded_at,omitempty"`
}

func toVideoResponse(v *repository.Video) VideoResponse {
	return VideoResponse{
		ID:              v.ID,
		JobID:           v.JobID,
		Filename:        v.Filename,
		DisplayName:     v.DisplayName,
		Status:          v.Status,
		Quality:         v.Quality,
		BroadcasterID:   v.BroadcasterID,
		StreamID:        v.StreamID,
		ViewerCount:     v.ViewerCount,
		Language:        v.Language,
		DurationSeconds: v.DurationSeconds,
		SizeBytes:       v.SizeBytes,
		Thumbnail:       v.Thumbnail,
		Error:           v.Error,
		StartDownloadAt: v.StartDownloadAt,
		DownloadedAt:    v.DownloadedAt,
	}
}

func toVideoResponses(vs []repository.Video) []VideoResponse {
	out := make([]VideoResponse, len(vs))
	for i := range vs {
		out[i] = toVideoResponse(&vs[i])
	}
	return out
}

// ListInput is the pagination input for video.list.
type ListInput struct {
	Limit  int    `json:"limit" validate:"min=0,max=200"`
	Offset int    `json:"offset" validate:"min=0"`
	Status string `json:"status" validate:"omitempty,oneof=PENDING RUNNING DONE FAILED"`
}

// List returns videos with optional status filter. Default limit 50.
func (s *Service) List(ctx context.Context, input ListInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	var (
		vids []repository.Video
		err  error
	)
	if input.Status != "" {
		vids, err = s.repo.ListVideosByStatus(ctx, input.Status, limit, input.Offset)
	} else {
		vids, err = s.repo.ListVideos(ctx, limit, input.Offset)
	}
	if err != nil {
		s.log.Error("list videos failed", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return toVideoResponses(vids), nil
}

// GetByIDInput wraps the integer video id.
type GetByIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

// GetByID returns a single video.
func (s *Service) GetByID(ctx context.Context, input GetByIDInput) (VideoResponse, error) {
	v, err := s.repo.GetVideo(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return VideoResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "video not found")
		}
		s.log.Error("get video failed", "error", err)
		return VideoResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get video")
	}
	return toVideoResponse(v), nil
}

// ByBroadcasterInput filters videos by broadcaster.
type ByBroadcasterInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	Limit         int    `json:"limit" validate:"min=0,max=200"`
	Offset        int    `json:"offset" validate:"min=0"`
}

// ByBroadcaster returns paginated videos for a broadcaster.
func (s *Service) ByBroadcaster(ctx context.Context, input ByBroadcasterInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	vids, err := s.repo.ListVideosByBroadcaster(ctx, input.BroadcasterID, limit, input.Offset)
	if err != nil {
		s.log.Error("list videos by broadcaster failed", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return toVideoResponses(vids), nil
}

// ByCategoryInput filters videos by category.
type ByCategoryInput struct {
	CategoryID string `json:"category_id" validate:"required"`
	Limit      int    `json:"limit" validate:"min=0,max=200"`
	Offset     int    `json:"offset" validate:"min=0"`
}

// ByCategory returns paginated videos in a category.
func (s *Service) ByCategory(ctx context.Context, input ByCategoryInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	vids, err := s.repo.ListVideosByCategory(ctx, input.CategoryID, limit, input.Offset)
	if err != nil {
		s.log.Error("list videos by category failed", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return toVideoResponses(vids), nil
}

// StatsBucket is one row of the status histogram.
type StatsBucket struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// StatisticsResponse is the aggregate shape for the dashboard home page.
type StatisticsResponse struct {
	Total         int64         `json:"total"`
	TotalSize     int64         `json:"total_size"`
	TotalDuration float64       `json:"total_duration_seconds"`
	ByStatus      []StatsBucket `json:"by_status"`
}

// Statistics returns totals + per-status counts.
func (s *Service) Statistics(ctx context.Context) (StatisticsResponse, error) {
	totals, err := s.repo.VideoStatsTotals(ctx)
	if err != nil {
		s.log.Error("stats totals failed", "error", err)
		return StatisticsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load statistics")
	}
	buckets, err := s.repo.VideoStatsByStatus(ctx)
	if err != nil {
		s.log.Error("stats by status failed", "error", err)
		return StatisticsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load statistics")
	}
	out := StatisticsResponse{
		Total:         totals.Total,
		TotalSize:     totals.TotalSize,
		TotalDuration: totals.TotalDuration,
		ByStatus:      make([]StatsBucket, len(buckets)),
	}
	for i, b := range buckets {
		out.ByStatus[i] = StatsBucket{Status: b.Status, Count: b.Count}
	}
	return out, nil
}

// TriggerDownloadInput starts a manual download for a live broadcaster.
type TriggerDownloadInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	Quality       string `json:"quality,omitempty" validate:"omitempty,oneof=LOW MEDIUM HIGH"`
}

// TriggerDownloadResponse returns the job id so the UI can subscribe to progress.
type TriggerDownloadResponse struct {
	JobID   string `json:"job_id"`
	VideoID int64  `json:"video_id"`
}

// TriggerDownload queues a new download. Admin-level: viewers can't trigger
// downloads. Uses the caller's user token so Helix rate-limit attribution
// goes to the right user.
func (s *Service) TriggerDownload(ctx context.Context, input TriggerDownloadInput) (TriggerDownloadResponse, error) {
	user := middleware.GetUser(ctx)
	tokens := middleware.GetTokens(ctx)
	if user == nil || tokens == nil {
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}

	quality := input.Quality
	if quality == "" {
		quality = repository.QualityHigh
	}

	// Channel record needs to exist so the foreign key on videos.broadcaster_id
	// is satisfied. If the admin hasn't synced this channel yet, tell them
	// rather than silently dropping the request.
	ch, err := s.repo.GetChannel(ctx, input.BroadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "channel not synced — run channel.syncFromTwitch first")
		}
		s.log.Error("get channel failed", "error", err)
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load channel")
	}

	// Attach user tokens + ID so Helix calls (from within the download flow)
	// are attributed and fetch logs get the right user.
	downloadCtx := twitch.WithUserToken(ctx, tokens.AccessToken)
	downloadCtx = twitch.WithUserID(downloadCtx, user.ID)

	jobID, err := s.downloader.Start(downloadCtx, downloader.Params{
		BroadcasterID:    ch.BroadcasterID,
		BroadcasterLogin: ch.BroadcasterLogin,
		DisplayName:      ch.BroadcasterName,
		Quality:          quality,
		Language:         derefString(ch.BroadcasterLanguage),
	})
	if err != nil {
		s.log.Error("start download failed", "error", err, "broadcaster_id", input.BroadcasterID)
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to start download: "+err.Error())
	}

	v, err := s.repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		// Row was just created — if we can't read it back something is wrong
		// with the DB, but the download itself is already queued.
		s.log.Error("reload video after start failed", "error", err, "job_id", jobID)
		return TriggerDownloadResponse{JobID: jobID}, nil
	}
	return TriggerDownloadResponse{JobID: jobID, VideoID: v.ID}, nil
}

// CancelInput asks the downloader to terminate an active job.
type CancelInput struct {
	JobID string `json:"job_id" validate:"required"`
}

// Cancel terminates an in-flight download. No-op if the job has finished.
func (s *Service) Cancel(ctx context.Context, input CancelInput) (OK, error) {
	s.downloader.Cancel(input.JobID)
	return OK{OK: true}, nil
}

// OK is a minimal ack response.
type OK struct {
	OK bool `json:"ok"`
}

// DownloadProgressInput identifies which job to stream progress for.
type DownloadProgressInput struct {
	JobID string `json:"job_id" validate:"required"`
}

// ProgressEvent is the wire shape for a download progress update. Matches
// downloader.Progress but pinned to a JSON-stable schema.
type ProgressEvent struct {
	JobID   string  `json:"job_id"`
	Stage   string  `json:"stage"`
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed,omitempty"`
	ETA     string  `json:"eta,omitempty"`
}

// DownloadProgress streams Progress events for a running download via SSE.
// Returns an empty closed channel if the job is not (or no longer) active —
// this collapses "already done" and "never existed" into a single tidy
// closed-stream path the client handles naturally.
//
// The trpcgo middleware chain runs before this handler, so an expired
// session will 401 on subscribe rather than hanging an open SSE forever.
// The SSE transport handles client disconnects by cancelling ctx, which
// propagates to our goroutine through the progressCh's closer (downloader
// already closes progressCh on completion).
func (s *Service) DownloadProgress(ctx context.Context, input DownloadProgressInput) (<-chan ProgressEvent, error) {
	src := s.downloader.Subscribe(input.JobID)
	if src == nil {
		// Return a pre-closed channel: the client's subscription completes
		// immediately, no error, which is the right UX for "nothing to
		// stream — the job is done or never existed."
		ch := make(chan ProgressEvent)
		close(ch)
		return ch, nil
	}

	out := make(chan ProgressEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				// Client disconnected or SSE max-duration hit. Leaving the
				// source channel alone is fine; the downloader publishes
				// with a non-blocking select so a dropped subscriber never
				// wedges its writes.
				return
			case p, ok := <-src:
				if !ok {
					return
				}
				select {
				case out <- ProgressEvent{
					JobID:   p.JobID,
					Stage:   p.Stage,
					Percent: p.Percent,
					Speed:   p.Speed,
					ETA:     p.ETA,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
