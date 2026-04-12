// Package video is the tRPC-transport wrapper around videoservice
// (metadata reads + aggregates) and downloadservice (control plane).
// stream.go holds the byte-range Chi handler.
package video

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/service/downloadservice"
	"github.com/befabri/replayvod/server/internal/service/videoservice"
	"github.com/befabri/trpcgo"
)

type Service struct {
	video    *videoservice.Service
	download *downloadservice.Service
	log      *slog.Logger
}

func NewService(video *videoservice.Service, download *downloadservice.Service, log *slog.Logger) *Service {
	return &Service{
		video:    video,
		download: download,
		log:      log.With("domain", "video-api"),
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

type ListInput struct {
	Limit  int    `json:"limit" validate:"min=0,max=200"`
	Offset int    `json:"offset" validate:"min=0"`
	Status string `json:"status,omitempty" validate:"omitempty,oneof=PENDING RUNNING DONE FAILED"`
}

func (s *Service) List(ctx context.Context, input ListInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	vids, err := s.video.List(ctx, input.Status, limit, input.Offset)
	if err != nil {
		s.log.Error("list videos", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return toVideoResponses(vids), nil
}

type GetByIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

func (s *Service) GetByID(ctx context.Context, input GetByIDInput) (VideoResponse, error) {
	v, err := s.video.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return VideoResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "video not found")
		}
		s.log.Error("get video", "error", err)
		return VideoResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get video")
	}
	return toVideoResponse(v), nil
}

type ByBroadcasterInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	Limit         int    `json:"limit" validate:"min=0,max=200"`
	Offset        int    `json:"offset" validate:"min=0"`
}

func (s *Service) ByBroadcaster(ctx context.Context, input ByBroadcasterInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	vids, err := s.video.ListByBroadcaster(ctx, input.BroadcasterID, limit, input.Offset)
	if err != nil {
		s.log.Error("list videos by broadcaster", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return toVideoResponses(vids), nil
}

type ByCategoryInput struct {
	CategoryID string `json:"category_id" validate:"required"`
	Limit      int    `json:"limit" validate:"min=0,max=200"`
	Offset     int    `json:"offset" validate:"min=0"`
}

func (s *Service) ByCategory(ctx context.Context, input ByCategoryInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	vids, err := s.video.ListByCategory(ctx, input.CategoryID, limit, input.Offset)
	if err != nil {
		s.log.Error("list videos by category", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return toVideoResponses(vids), nil
}

type StatsBucket struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type StatisticsResponse struct {
	Total         int64         `json:"total"`
	TotalSize     int64         `json:"total_size"`
	TotalDuration float64       `json:"total_duration_seconds"`
	ByStatus      []StatsBucket `json:"by_status"`
}

func (s *Service) Statistics(ctx context.Context) (StatisticsResponse, error) {
	stats, err := s.video.Statistics(ctx)
	if err != nil {
		s.log.Error("video statistics", "error", err)
		return StatisticsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load statistics")
	}
	out := StatisticsResponse{
		Total:         stats.Totals.Total,
		TotalSize:     stats.Totals.TotalSize,
		TotalDuration: stats.Totals.TotalDuration,
		ByStatus:      make([]StatsBucket, len(stats.ByStatus)),
	}
	for i, b := range stats.ByStatus {
		out.ByStatus[i] = StatsBucket{Status: b.Status, Count: b.Count}
	}
	return out, nil
}

// TriggerDownloadInput starts a manual download for a live broadcaster.
// RecordingType + ForceH264 are accepted at the API boundary so the
// dashboard can send them; the native HLS downloader (Phase 4+) will
// consume them at Stage 3 variant selection. Until then they are
// recorded on the `videos` row via VideoInput but otherwise ignored.
type TriggerDownloadInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	RecordingType string `json:"recording_type,omitempty" validate:"omitempty,oneof=video audio"`
	Quality       string `json:"quality,omitempty" validate:"omitempty,oneof=LOW MEDIUM HIGH"`
	ForceH264     bool   `json:"force_h264,omitempty"`
}

// TriggerDownloadResponse returns the job id so the UI can subscribe
// to progress.
type TriggerDownloadResponse struct {
	JobID   string `json:"job_id"`
	VideoID int64  `json:"video_id"`
}

func (s *Service) TriggerDownload(ctx context.Context, input TriggerDownloadInput) (TriggerDownloadResponse, error) {
	user := middleware.GetUser(ctx)
	tokens := middleware.GetTokens(ctx)
	if user == nil || tokens == nil {
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	result, err := s.download.Trigger(ctx, downloadservice.TriggerInput{
		BroadcasterID:   input.BroadcasterID,
		RecordingType:   input.RecordingType,
		Quality:         input.Quality,
		ForceH264:       input.ForceH264,
		UserID:          user.ID,
		UserAccessToken: tokens.AccessToken,
	})
	if err != nil {
		if errors.Is(err, downloadservice.ErrChannelNotSynced) {
			return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeNotFound,
				"channel not synced — run channel.syncFromTwitch first")
		}
		s.log.Error("trigger download", "error", err, "broadcaster_id", input.BroadcasterID)
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError,
			"failed to start download: "+err.Error())
	}
	return TriggerDownloadResponse{JobID: result.JobID, VideoID: result.VideoID}, nil
}

type CancelInput struct {
	JobID string `json:"job_id" validate:"required"`
}

type OK struct {
	OK bool `json:"ok"`
}

func (s *Service) Cancel(ctx context.Context, input CancelInput) (OK, error) {
	s.download.Cancel(ctx, input.JobID)
	return OK{OK: true}, nil
}

type DownloadProgressInput struct {
	JobID string `json:"job_id" validate:"required"`
}

// ProgressEvent is the wire shape for a download progress update.
// Matches downloader.Progress but pinned to a JSON-stable schema.
type ProgressEvent struct {
	JobID   string  `json:"job_id"`
	Stage   string  `json:"stage"`
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed,omitempty"`
	ETA     string  `json:"eta,omitempty"`
}

// DownloadProgress streams Progress events for a running download
// via SSE. Returns an empty closed channel if the job is not (or no
// longer) active — this collapses "already done" and "never existed"
// into a single tidy closed-stream path the client handles naturally.
//
// The trpcgo middleware chain runs before this handler, so an expired
// session 401s on subscribe rather than hanging an open SSE forever.
// The SSE transport handles client disconnects by cancelling ctx,
// which propagates through to us; the downloader publishes with a
// non-blocking select so a dropped subscriber never wedges its writes.
func (s *Service) DownloadProgress(ctx context.Context, input DownloadProgressInput) (<-chan ProgressEvent, error) {
	src := s.download.Subscribe(input.JobID)
	if src == nil {
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
