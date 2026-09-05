// Package schedule implements the schedule.* tRPC procedures. All
// business logic (authorization, filter validation, category/tag
// junction replacement) lives in internal/service/schedule — the
// domain service is shared with the webhook processor.
package schedule

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the schedule domain. Role-based
// visibility (owner vs author) is forwarded as a boolean rather than
// re-inspected in the service — keeps middleware as the single source
// of truth on role semantics.
type Handler struct {
	svc *schedulesvc.Service
	log *slog.Logger
}

func NewHandler(svc *schedulesvc.Service, log *slog.Logger) *Handler {
	return &Handler{
		svc: svc,
		log: log.With("domain", "schedule"),
	}
}

// ScheduleResponse is the wire shape the dashboard consumes. Categories
// and tags are inlined so the list page doesn't have to N+1 per row.
type ScheduleResponse struct {
	ID            int64  `json:"id"`
	BroadcasterID string `json:"broadcaster_id"`
	RequestedBy   string `json:"requested_by"`
	// RequestedFrom is absent for directly created schedules.
	RequestedFrom     *string        `json:"requested_from,omitempty"`
	RequestedFromName string         `json:"requested_from_name"`
	RecordingType     string         `json:"recording_type"`
	Quality           string         `json:"quality"`
	ForceH264         bool           `json:"force_h264"`
	HasMinViewers     bool           `json:"has_min_viewers"`
	MinViewers        *int64         `json:"min_viewers,omitempty"`
	HasCategories     bool           `json:"has_categories"`
	HasTags           bool           `json:"has_tags"`
	IsDeleteRediff    bool           `json:"is_delete_rediff"`
	TimeBeforeDelete  *int64         `json:"time_before_delete,omitempty"`
	IsDisabled        bool           `json:"is_disabled"`
	LastTriggeredAt   *time.Time     `json:"last_triggered_at,omitempty"`
	TriggerCount      int64          `json:"trigger_count"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	Categories        []CategoryLink `json:"categories"`
	Tags              []TagLink      `json:"tags"`
}

type CategoryLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TagLink struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// toResponse converts a domain View into the wire shape. Kept in the
// transport layer so domain types don't carry JSON tags.
func toResponse(v schedulesvc.View) ScheduleResponse {
	resp := ScheduleResponse{
		ID:                v.Schedule.ID,
		BroadcasterID:     v.Schedule.BroadcasterID,
		RequestedBy:       v.Schedule.RequestedBy,
		RequestedFrom:     v.Schedule.RequestedFrom,
		RequestedFromName: v.RequestedFromName,
		RecordingType:     v.Schedule.RecordingType,
		Quality:           v.Schedule.Quality,
		ForceH264:         v.Schedule.ForceH264,
		HasMinViewers:     v.Schedule.HasMinViewers,
		MinViewers:        v.Schedule.MinViewers,
		HasCategories:     v.Schedule.HasCategories,
		HasTags:           v.Schedule.HasTags,
		IsDeleteRediff:    v.Schedule.IsDeleteRediff,
		TimeBeforeDelete:  v.Schedule.TimeBeforeDelete,
		IsDisabled:        v.Schedule.IsDisabled,
		LastTriggeredAt:   v.Schedule.LastTriggeredAt,
		TriggerCount:      v.Schedule.TriggerCount,
		CreatedAt:         v.Schedule.CreatedAt,
		UpdatedAt:         v.Schedule.UpdatedAt,
		Categories:        make([]CategoryLink, 0, len(v.Categories)),
		Tags:              make([]TagLink, 0, len(v.Tags)),
	}
	for _, c := range v.Categories {
		resp.Categories = append(resp.Categories, CategoryLink{ID: c.ID, Name: c.Name})
	}
	for _, t := range v.Tags {
		resp.Tags = append(resp.Tags, TagLink{ID: t.ID, Name: t.Name})
	}
	return resp
}

// scheduleErrRules map the schedule domain sentinels to tRPC codes, shared by
// every handler in this package. ErrInvalidFilter surfaces its own validation
// message verbatim; ErrNotFound/ErrNotOwner get fixed operator-facing strings.
var scheduleErrRules = []apierr.Rule{
	apierr.On(repository.ErrNotFound, trpcgo.CodeNotFound, "schedule not found"),
	apierr.On(schedulesvc.ErrNotOwner, trpcgo.CodeForbidden, "not your schedule"),
	apierr.OnVerbatim(schedulesvc.ErrInvalidFilter, trpcgo.CodeBadRequest),
	apierr.On(schedulesvc.ErrAlreadyScheduled, trpcgo.CodeBadRequest, "channel already has a schedule"),
}

type ListInput struct {
	Limit  int `json:"limit" validate:"min=0,max=200"`
	Offset int `json:"offset" validate:"min=0"`
}

type ListResponse struct {
	Data []ScheduleResponse `json:"data"`
}

// List returns a page of schedules across all users.
func (h *Handler) List(ctx context.Context, input ListInput) (ListResponse, error) {
	if _, err := middleware.RequireUser(ctx); err != nil {
		return ListResponse{}, err
	}
	views, err := h.svc.List(ctx, input.Limit, input.Offset)
	if err != nil {
		return ListResponse{}, apierr.Map(h.log, err, "list schedules", scheduleErrRules...)
	}
	return ListResponse{Data: toResponses(views)}, nil
}

// Mine returns a page of schedules owned by the caller.
func (h *Handler) Mine(ctx context.Context, input ListInput) (ListResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ListResponse{}, err
	}
	views, err := h.svc.Mine(ctx, user.ID, input.Limit, input.Offset)
	if err != nil {
		return ListResponse{}, apierr.Map(h.log, err, "list schedules", scheduleErrRules...)
	}
	return ListResponse{Data: toResponses(views)}, nil
}

type GetByIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

// GetByID returns a single schedule. Below admin, callers may only
// load their own.
func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (ScheduleResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.GetByID(ctx, user.ID, middleware.HasMinRole(user.Role, middleware.RoleAdmin), input.ID)
	if err != nil {
		return ScheduleResponse{}, apierr.Map(h.log, err, "load schedule", scheduleErrRules...)
	}
	return toResponse(*view), nil
}

// CreateInput captures the full schedule payload the dashboard posts. The
// CHECK constraints in the schema enforce that each has_X toggle has its
// corresponding value present; surfacing a tight 400 at the tRPC boundary
// keeps the UI simpler than wrapping driver-level constraint errors.
type CreateInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	ScheduleSettingsInput
}

// Create registers a schedule for the caller. requested_by is always the
// caller; admins cannot create a schedule on someone else's behalf — role
// boundaries stay intact at the service layer.
func (h *Handler) Create(ctx context.Context, input CreateInput) (ScheduleResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.Create(ctx, user.ID, input.writeInput(input.BroadcasterID))
	if err != nil {
		return ScheduleResponse{}, apierr.Map(h.log, err, "create schedule", scheduleErrRules...)
	}
	return toResponse(*view), nil
}

// UpdateInput mirrors CreateInput plus the schedule ID. We don't allow
// changing broadcaster_id — that would effectively move the schedule to
// another channel and should be a delete+create instead.
type UpdateInput struct {
	ID int64 `json:"id" validate:"required"`
	ScheduleSettingsInput
}

// Update changes schedule settings while preserving trigger history. The caller
// must own the schedule or have admin access.
func (h *Handler) Update(ctx context.Context, input UpdateInput) (ScheduleResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.Update(ctx, user.ID, middleware.HasMinRole(user.Role, middleware.RoleAdmin), input.ID, input.writeInput(""))
	if err != nil {
		return ScheduleResponse{}, apierr.Map(h.log, err, "update schedule", scheduleErrRules...)
	}
	return toResponse(*view), nil
}

type ToggleInput struct {
	ID int64 `json:"id" validate:"required"`
}

// Toggle flips is_disabled in one atomic UPDATE so the dashboard checkbox
// can POST without re-sending the full payload.
func (h *Handler) Toggle(ctx context.Context, input ToggleInput) (ScheduleResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.Toggle(ctx, user.ID, middleware.HasMinRole(user.Role, middleware.RoleAdmin), input.ID)
	if err != nil {
		return ScheduleResponse{}, apierr.Map(h.log, err, "toggle schedule", scheduleErrRules...)
	}
	return toResponse(*view), nil
}

type DeleteInput struct {
	ID int64 `json:"id" validate:"required"`
}

type DeleteResponse struct {
	ID int64 `json:"id"`
}

// Delete removes a schedule and its junction rows (ON DELETE CASCADE).
func (h *Handler) Delete(ctx context.Context, input DeleteInput) (DeleteResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return DeleteResponse{}, err
	}
	if err := h.svc.Delete(ctx, user.ID, middleware.HasMinRole(user.Role, middleware.RoleAdmin), input.ID); err != nil {
		return DeleteResponse{}, apierr.Map(h.log, err, "delete schedule", scheduleErrRules...)
	}
	return DeleteResponse{ID: input.ID}, nil
}

// PauseStateResponse reports the global auto-download pause flag. The dashboard
// uses it to render the Pause all / Resume button and the paused banner.
type PauseStateResponse struct {
	Paused bool `json:"paused"`
}

// PauseState returns whether auto-downloads are globally paused. Viewer-level:
// the paused banner is visible to anyone who can see the schedules list.
func (h *Handler) PauseState(ctx context.Context) (PauseStateResponse, error) {
	if _, err := middleware.RequireUser(ctx); err != nil {
		return PauseStateResponse{}, err
	}
	paused, err := h.svc.PausedState(ctx)
	if err != nil {
		return PauseStateResponse{}, apierr.Map(h.log, err, "schedule pause state", scheduleErrRules...)
	}
	return PauseStateResponse{Paused: paused}, nil
}

type SetPausedInput struct {
	Paused bool `json:"paused"`
}

// SetPaused flips the global auto-download pause flag. Admin-only, mirroring the
// other schedule writes so viewers can't halt every recording.
func (h *Handler) SetPaused(ctx context.Context, input SetPausedInput) (PauseStateResponse, error) {
	if _, err := middleware.RequireUser(ctx); err != nil {
		return PauseStateResponse{}, err
	}
	paused, err := h.svc.SetPaused(ctx, input.Paused)
	if err != nil {
		return PauseStateResponse{}, apierr.Map(h.log, err, "set schedule pause", scheduleErrRules...)
	}
	return PauseStateResponse{Paused: paused}, nil
}

func toResponses(views []schedulesvc.View) []ScheduleResponse {
	out := make([]ScheduleResponse, 0, len(views))
	for _, v := range views {
		out = append(out, toResponse(v))
	}
	return out
}
