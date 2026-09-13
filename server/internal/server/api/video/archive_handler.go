package video

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// ArchiveEnqueueStatus is the wire form of EnqueueStatus.
type ArchiveEnqueueStatus string

const (
	ArchiveEnqueueQueued   ArchiveEnqueueStatus = ArchiveEnqueueStatus(EnqueueQueued)
	ArchiveEnqueueExists   ArchiveEnqueueStatus = ArchiveEnqueueStatus(EnqueueExists)
	ArchiveEnqueueNotFound ArchiveEnqueueStatus = ArchiveEnqueueStatus(EnqueueNotFound)
	ArchiveEnqueueInvalid  ArchiveEnqueueStatus = ArchiveEnqueueStatus(EnqueueInvalid)
	ArchiveEnqueueLive     ArchiveEnqueueStatus = ArchiveEnqueueStatus(EnqueueLive)
	ArchiveEnqueuePrivate  ArchiveEnqueueStatus = ArchiveEnqueueStatus(EnqueuePrivate)
	ArchiveEnqueueError    ArchiveEnqueueStatus = ArchiveEnqueueStatus(EnqueueError)
)

// ArchiveHeldReason is the wire form of HeldReason.
type ArchiveHeldReason string

const (
	ArchiveHeldByArchive       ArchiveHeldReason = ArchiveHeldReason(HeldByArchive)
	ArchiveHeldByLiveRecording ArchiveHeldReason = ArchiveHeldReason(HeldByLiveRecording)
)

type ChannelVODsInput struct {
	// Channel accepts a Twitch login or twitch.tv channel URL.
	Channel string `json:"channel" validate:"required"`
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
}

type ArchiveChannelResponse struct {
	BroadcasterID   string `json:"broadcaster_id"`
	Login           string `json:"login"`
	Name            string `json:"name"`
	ProfileImageURL string `json:"profile_image_url,omitempty"`
}

type TwitchVODResponse struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	URL             string    `json:"url"`
	Type            string    `json:"type"`
	CreatedAt       time.Time `json:"created_at"`
	DurationSeconds int       `json:"duration_seconds"`
	ThumbnailURL    string    `json:"thumbnail_url,omitempty"`
	ViewCount       int       `json:"view_count"`
	Language        string    `json:"language,omitempty"`
	// Viewable is Twitch's "public" or "private"; private VODs cannot be archived.
	Viewable string `json:"viewable,omitempty"`
	// Live VODs cannot be archived until the broadcast ends.
	Live bool `json:"live,omitempty"`
	// ArchivedVideoID and ArchivedStatus include queued, active, and finished
	// recordings; HeldReason distinguishes an archive from a live recording.
	ArchivedVideoID *int64             `json:"archived_video_id,omitempty"`
	ArchivedStatus  *VideoStatus       `json:"archived_status,omitempty"`
	HeldReason      *ArchiveHeldReason `json:"held_reason,omitempty"`
}

type ChannelVODsResponse struct {
	Channel    ArchiveChannelResponse `json:"channel"`
	VODs       []TwitchVODResponse    `json:"vods"`
	NextCursor *string                `json:"next_cursor,omitempty"`
}

func (h *Handler) ListChannelVODs(ctx context.Context, input ChannelVODsInput) (ChannelVODsResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ChannelVODsResponse{}, err
	}
	login, ok := ParseChannelLogin(input.Channel)
	if !ok {
		return ChannelVODsResponse{}, trpcgo.NewError(trpcgo.CodeBadRequest, "enter a Twitch channel name or channel link")
	}
	page, err := h.archive.ListChannelVODs(ctx, user.ID, login, input.Cursor, input.Limit)
	if err != nil {
		return ChannelVODsResponse{}, apierr.Map(h.log, err, "list channel vods",
			apierr.On(ErrChannelNotFound, trpcgo.CodeNotFound, "no Twitch channel with that name"))
	}
	resp := ChannelVODsResponse{
		Channel: ArchiveChannelResponse{
			BroadcasterID:   page.Channel.ID,
			Login:           page.Channel.Login,
			Name:            page.Channel.DisplayName,
			ProfileImageURL: page.Channel.ProfileImageURL,
		},
		VODs: make([]TwitchVODResponse, 0, len(page.VODs)),
	}
	if page.NextCursor != "" {
		cursor := page.NextCursor
		resp.NextCursor = &cursor
	}
	for _, v := range page.VODs {
		row := TwitchVODResponse{
			ID:              v.ID,
			Title:           v.Title,
			URL:             v.URL,
			Type:            v.Type,
			CreatedAt:       v.CreatedAt,
			DurationSeconds: v.DurationSeconds,
			ThumbnailURL:    v.ThumbnailURL,
			ViewCount:       v.ViewCount,
			Language:        v.Language,
			Viewable:        v.Viewable,
			Live:            v.Live,
		}
		if v.Held != nil {
			id := v.Held.ID
			status := VideoStatus(v.Held.Status)
			reason := ArchiveHeldReason(v.HeldReason)
			row.ArchivedVideoID = &id
			row.ArchivedStatus = &status
			row.HeldReason = &reason
		}
		resp.VODs = append(resp.VODs, row)
	}
	return resp, nil
}

type EnqueueArchiveInput struct {
	// VODs accepts Twitch VOD links or IDs, one per entry.
	VODs          []string `json:"vods" validate:"required,min=1,max=50,dive,required"`
	RecordingType string   `json:"recording_type,omitempty" validate:"omitempty,oneof=video audio"`
	Quality       string   `json:"quality,omitempty" validate:"omitempty,oneof=LOW MEDIUM HIGH 1440 BEST"`
	ForceH264     bool     `json:"force_h264,omitempty"`
}

type EnqueueArchiveItem struct {
	Input   string               `json:"input"`
	VODID   string               `json:"vod_id,omitempty"`
	Status  ArchiveEnqueueStatus `json:"status"`
	Title   string               `json:"title,omitempty"`
	VideoID *int64               `json:"video_id,omitempty"`
	JobID   string               `json:"job_id,omitempty"`
	Message string               `json:"message,omitempty"`
}

type EnqueueArchiveResponse struct {
	Items []EnqueueArchiveItem `json:"items"`
}

func (h *Handler) EnqueueArchive(ctx context.Context, input EnqueueArchiveInput) (EnqueueArchiveResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return EnqueueArchiveResponse{}, err
	}
	items, err := h.archive.Enqueue(ctx, EnqueueInput{
		Inputs:        input.VODs,
		RecordingType: input.RecordingType,
		Quality:       input.Quality,
		ForceH264:     input.ForceH264,
		UserID:        user.ID,
	})
	if err != nil {
		return EnqueueArchiveResponse{}, apierr.Map(h.log, err, "queue archives")
	}
	resp := EnqueueArchiveResponse{Items: make([]EnqueueArchiveItem, 0, len(items))}
	for _, it := range items {
		row := EnqueueArchiveItem{
			Input:   it.Input,
			VODID:   it.VODID,
			Status:  ArchiveEnqueueStatus(it.Status),
			Title:   it.Title,
			JobID:   it.JobID,
			Message: it.Message,
		}
		if it.VideoID != 0 {
			id := it.VideoID
			row.VideoID = &id
		}
		resp.Items = append(resp.Items, row)
	}
	return resp, nil
}

// ArchiveQueueResponse lists queued and running archives oldest first and
// failures from the last seven days newest first; NextRetryAt marks scheduled retries.
type ArchiveQueueResponse struct {
	Queue    []VideoResponse `json:"queue"`
	Failures []VideoResponse `json:"failures"`
}

func (h *Handler) ArchiveQueue(ctx context.Context) (ArchiveQueueResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ArchiveQueueResponse{}, err
	}
	q, err := h.archive.Queue(ctx)
	if err != nil {
		return ArchiveQueueResponse{}, apierr.Map(h.log, err, "list archive queue")
	}
	queue, err := h.toVideoResponses(ctx, user.ID, q.Queue)
	if err != nil {
		return ArchiveQueueResponse{}, err
	}
	failures, err := h.toVideoResponses(ctx, user.ID, q.Failures)
	if err != nil {
		return ArchiveQueueResponse{}, err
	}
	return ArchiveQueueResponse{
		Queue:    queue,
		Failures: failures,
	}, nil
}

type ArchiveVideoInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

func (h *Handler) DequeueArchive(ctx context.Context, input ArchiveVideoInput) (OK, error) {
	if _, err := middleware.RequireUser(ctx); err != nil {
		return OK{}, err
	}
	if err := h.archive.Dequeue(ctx, input.VideoID); err != nil {
		return OK{}, apierr.Map(h.log, err, "remove queued archive",
			apierr.On(downloader.ErrBusy, trpcgo.CodeConflict, "this archive is already downloading; cancel it instead"),
			apierr.On(repository.ErrNotFound, trpcgo.CodeNotFound, "no queued archive with that id"))
	}
	return OK{OK: true}, nil
}

// RetryArchive queues a new attempt of a failed archive right away.
func (h *Handler) RetryArchive(ctx context.Context, input ArchiveVideoInput) (OK, error) {
	if _, err := middleware.RequireUser(ctx); err != nil {
		return OK{}, err
	}
	if err := h.archive.Retry(ctx, input.VideoID); err != nil {
		return OK{}, apierr.Map(h.log, err, "retry archive",
			apierr.On(downloader.ErrBusy, trpcgo.CodeConflict, "this archive is still winding down; try again in a moment"),
			apierr.On(repository.ErrDuplicate, trpcgo.CodeConflict, "this VOD is already queued or in the library"),
			apierr.On(repository.ErrNotFound, trpcgo.CodeNotFound, "no failed archive with that id"))
	}
	return OK{OK: true}, nil
}

// CancelArchiveRetry drops the scheduled retry of a failed archive.
func (h *Handler) CancelArchiveRetry(ctx context.Context, input ArchiveVideoInput) (OK, error) {
	if _, err := middleware.RequireUser(ctx); err != nil {
		return OK{}, err
	}
	if err := h.archive.CancelRetry(ctx, input.VideoID); err != nil {
		return OK{}, apierr.Map(h.log, err, "cancel archive retry",
			apierr.On(repository.ErrNotFound, trpcgo.CodeNotFound, "no retry is scheduled for that archive"))
	}
	return OK{OK: true}, nil
}
