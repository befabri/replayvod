package schedule

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/trpcgo"
)

// ScheduleRequestStatus is the request lifecycle enum exposed to API clients.
type ScheduleRequestStatus string

const (
	ScheduleRequestStatusPending  ScheduleRequestStatus = repository.ScheduleRequestStatusPending
	ScheduleRequestStatusApproved ScheduleRequestStatus = repository.ScheduleRequestStatusApproved
	ScheduleRequestStatusRejected ScheduleRequestStatus = repository.ScheduleRequestStatusRejected
)

type ScheduleRequestResponse struct {
	ID               int64                 `json:"id"`
	BroadcasterID    string                `json:"broadcaster_id"`
	BroadcasterLogin string                `json:"broadcaster_login"`
	BroadcasterName  string                `json:"broadcaster_name"`
	ProfileImageURL  *string               `json:"profile_image_url,omitempty"`
	RequestedBy      string                `json:"requested_by"`
	RequestedByName  string                `json:"requested_by_name"`
	Note             *string               `json:"note,omitempty"`
	Status           ScheduleRequestStatus `json:"status"`
	ScheduleID       *int64                `json:"schedule_id,omitempty"`
	DecidedBy        *string               `json:"decided_by,omitempty"`
	DecidedAt        *time.Time            `json:"decided_at,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
}

func toRequestResponse(v repository.ScheduleRequestView) ScheduleRequestResponse {
	return ScheduleRequestResponse{
		ID:               v.ID,
		BroadcasterID:    v.BroadcasterID,
		BroadcasterLogin: v.BroadcasterLogin,
		BroadcasterName:  v.BroadcasterName,
		ProfileImageURL:  v.ProfileImageURL,
		RequestedBy:      v.RequestedBy,
		RequestedByName:  v.RequestedByName,
		Note:             v.Note,
		Status:           ScheduleRequestStatus(v.Status),
		ScheduleID:       v.ScheduleID,
		DecidedBy:        v.DecidedBy,
		DecidedAt:        v.DecidedAt,
		CreatedAt:        v.CreatedAt,
	}
}

func toRequestResponses(views []repository.ScheduleRequestView) []ScheduleRequestResponse {
	out := make([]ScheduleRequestResponse, len(views))
	for i, v := range views {
		out[i] = toRequestResponse(v)
	}
	return out
}

var requestErrRules = []apierr.Rule{
	apierr.On(schedulesvc.ErrRequestNotFound, trpcgo.CodeNotFound, "request not found"),
	apierr.On(schedulesvc.ErrRequestAlreadyDecided, trpcgo.CodeBadRequest, "request already decided"),
	apierr.On(schedulesvc.ErrRequestAlreadyExists, trpcgo.CodeBadRequest, "you already requested this channel"),
	apierr.On(schedulesvc.ErrAlreadyScheduled, trpcgo.CodeBadRequest, "channel already has a schedule"),
	apierr.On(repository.ErrNotFound, trpcgo.CodeNotFound, "channel not found"),
}

type CreateRequestInput struct {
	BroadcasterID string  `json:"broadcaster_id" validate:"required"`
	Note          *string `json:"note,omitempty" validate:"omitempty,max=200"`
}

type RequestOK struct {
	OK bool `json:"ok"`
}

// CreateRequest submits a recording request for the caller.
func (h *Handler) CreateRequest(ctx context.Context, input CreateRequestInput) (RequestOK, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return RequestOK{}, err
	}
	if _, err := h.svc.CreateRequest(ctx, user.ID, input.BroadcasterID, input.Note); err != nil {
		return RequestOK{}, apierr.Map(h.log, err, "create schedule request", requestErrRules...)
	}
	return RequestOK{OK: true}, nil
}

type RequestPageCursor struct {
	CreatedAt time.Time `json:"created_at" validate:"required"`
	ID        int64     `json:"id" validate:"required,min=1"`
}

type ListRequestsInput struct {
	Limit  int                `json:"limit,omitempty" validate:"min=0,max=200"`
	Cursor *RequestPageCursor `json:"cursor,omitempty" validate:"omitempty"`
}

func (input ListRequestsInput) repositoryCursor() *repository.ScheduleRequestCursor {
	if input.Cursor == nil {
		return nil
	}
	return &repository.ScheduleRequestCursor{CreatedAt: input.Cursor.CreatedAt, ID: input.Cursor.ID}
}

type RequestPageResponse struct {
	Items      []ScheduleRequestResponse `json:"items"`
	NextCursor *RequestPageCursor        `json:"next_cursor,omitempty"`
}

func toRequestPage(page schedulesvc.RequestPage) RequestPageResponse {
	out := RequestPageResponse{Items: toRequestResponses(page.Items)}
	if page.NextCursor != nil {
		out.NextCursor = &RequestPageCursor{CreatedAt: page.NextCursor.CreatedAt, ID: page.NextCursor.ID}
	}
	return out
}

// MyRequests returns the caller's request history, newest first.
func (h *Handler) MyRequests(ctx context.Context, input ListRequestsInput) (RequestPageResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return RequestPageResponse{}, err
	}
	page, err := h.svc.ListRequestsForUser(ctx, user.ID, input.Limit, input.repositoryCursor())
	if err != nil {
		return RequestPageResponse{}, apierr.Map(h.log, err, "list my schedule requests", requestErrRules...)
	}
	return toRequestPage(page), nil
}

// Requests returns pending and decided requests for admin review.
func (h *Handler) Requests(ctx context.Context, input ListRequestsInput) (RequestPageResponse, error) {
	page, err := h.svc.ListRequests(ctx, input.Limit, input.repositoryCursor())
	if err != nil {
		return RequestPageResponse{}, apierr.Map(h.log, err, "list schedule requests", requestErrRules...)
	}
	return toRequestPage(page), nil
}

// ApproveRequestInput supplies settings for the request's channel.
type ApproveRequestInput struct {
	RequestID int64 `json:"request_id" validate:"required"`
	ScheduleSettingsInput
}

// ApproveRequest creates the schedule and consumes the request.
func (h *Handler) ApproveRequest(ctx context.Context, input ApproveRequestInput) (ScheduleResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.ApproveRequest(ctx, user.ID, input.RequestID, input.writeInput(""))
	if err != nil {
		return ScheduleResponse{}, apierr.Map(h.log, err, "approve schedule request",
			append(requestErrRules, scheduleErrRules...)...)
	}
	return toResponse(*view), nil
}

type RequestIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

// RejectRequest closes a pending request without creating a schedule.
func (h *Handler) RejectRequest(ctx context.Context, input RequestIDInput) (RequestOK, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return RequestOK{}, err
	}
	if err := h.svc.RejectRequest(ctx, user.ID, input.ID); err != nil {
		return RequestOK{}, apierr.Map(h.log, err, "reject schedule request", requestErrRules...)
	}
	return RequestOK{OK: true}, nil
}

// CancelRequest withdraws the caller's own pending request.
func (h *Handler) CancelRequest(ctx context.Context, input RequestIDInput) (RequestOK, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return RequestOK{}, err
	}
	if err := h.svc.CancelRequest(ctx, user.ID, input.ID); err != nil {
		return RequestOK{}, apierr.Map(h.log, err, "cancel schedule request", requestErrRules...)
	}
	return RequestOK{OK: true}, nil
}
