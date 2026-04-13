// Package schedule implements the schedule.* tRPC procedures. All
// business logic (authorization, filter validation, category/tag
// junction replacement) lives in internal/service/schedule — the
// domain service is shared with the webhook processor.
package schedule

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
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

// NewHandler wires the tRPC adapter onto the schedule domain service.
func NewHandler(svc *schedulesvc.Service, log *slog.Logger) *Handler {
	return &Handler{
		svc: svc,
		log: log.With("domain", "schedule"),
	}
}

// ScheduleResponse is the wire shape the dashboard consumes. Categories
// and tags are inlined so the list page doesn't have to N+1 per row.
type ScheduleResponse struct {
	ID               int64          `json:"id"`
	BroadcasterID    string         `json:"broadcaster_id"`
	RequestedBy      string         `json:"requested_by"`
	Quality          string         `json:"quality"`
	HasMinViewers    bool           `json:"has_min_viewers"`
	MinViewers       *int64         `json:"min_viewers,omitempty"`
	HasCategories    bool           `json:"has_categories"`
	HasTags          bool           `json:"has_tags"`
	IsDeleteRediff   bool           `json:"is_delete_rediff"`
	TimeBeforeDelete *int64         `json:"time_before_delete,omitempty"`
	IsDisabled       bool           `json:"is_disabled"`
	LastTriggeredAt  *time.Time     `json:"last_triggered_at,omitempty"`
	TriggerCount     int64          `json:"trigger_count"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	Categories       []CategoryLink `json:"categories"`
	Tags             []TagLink      `json:"tags"`
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
		ID:               v.Schedule.ID,
		BroadcasterID:    v.Schedule.BroadcasterID,
		RequestedBy:      v.Schedule.RequestedBy,
		Quality:          v.Schedule.Quality,
		HasMinViewers:    v.Schedule.HasMinViewers,
		MinViewers:       v.Schedule.MinViewers,
		HasCategories:    v.Schedule.HasCategories,
		HasTags:          v.Schedule.HasTags,
		IsDeleteRediff:   v.Schedule.IsDeleteRediff,
		TimeBeforeDelete: v.Schedule.TimeBeforeDelete,
		IsDisabled:       v.Schedule.IsDisabled,
		LastTriggeredAt:  v.Schedule.LastTriggeredAt,
		TriggerCount:     v.Schedule.TriggerCount,
		CreatedAt:        v.Schedule.CreatedAt,
		UpdatedAt:        v.Schedule.UpdatedAt,
		Categories:       make([]CategoryLink, 0, len(v.Categories)),
		Tags:             make([]TagLink, 0, len(v.Tags)),
	}
	for _, c := range v.Categories {
		resp.Categories = append(resp.Categories, CategoryLink{ID: c.ID, Name: c.Name})
	}
	for _, t := range v.Tags {
		resp.Tags = append(resp.Tags, TagLink{ID: t.ID, Name: t.Name})
	}
	return resp
}

// mapErr translates schedule sentinels to tRPC codes. Anything not
// specifically mapped is logged and surfaced as a 500 — callers pass
// an action verb for the log + operator-facing message.
func (h *Handler) mapErr(err error, action string) error {
	if errors.Is(err, repository.ErrNotFound) {
		return trpcgo.NewError(trpcgo.CodeNotFound, "schedule not found")
	}
	if errors.Is(err, schedulesvc.ErrNotOwner) {
		return trpcgo.NewError(trpcgo.CodeForbidden, "not your schedule")
	}
	if errors.Is(err, schedulesvc.ErrInvalidFilter) {
		return trpcgo.NewError(trpcgo.CodeBadRequest, err.Error())
	}
	h.log.Error(action, "error", err)
	return trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to "+action)
}

// callerContext extracts the authenticated user. Returns a tRPC auth
// error when no session is attached.
func callerContext(ctx context.Context) (*repository.User, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	return user, nil
}

type ListInput struct {
	Limit  int `json:"limit" validate:"min=0,max=200"`
	Offset int `json:"offset" validate:"min=0"`
}

type ListResponse struct {
	Data []ScheduleResponse `json:"data"`
}

// List returns schedules the caller can see. Owners see all; admins and
// viewers see only their own. We intentionally don't paginate on a total
// count because the expected cardinality is low (one per user per channel).
func (h *Handler) List(ctx context.Context, input ListInput) (ListResponse, error) {
	user, err := callerContext(ctx)
	if err != nil {
		return ListResponse{}, err
	}
	views, err := h.svc.List(ctx, user.ID, user.Role == middleware.RoleOwner, input.Limit, input.Offset)
	if err != nil {
		return ListResponse{}, h.mapErr(err, "list schedules")
	}
	return ListResponse{Data: toResponses(views)}, nil
}

// Mine returns schedules owned by the calling user. Distinct from List
// so a future public API can expose it to viewers without granting the
// system-wide list that owners have.
func (h *Handler) Mine(ctx context.Context, input ListInput) (ListResponse, error) {
	user, err := callerContext(ctx)
	if err != nil {
		return ListResponse{}, err
	}
	views, err := h.svc.Mine(ctx, user.ID, input.Limit, input.Offset)
	if err != nil {
		return ListResponse{}, h.mapErr(err, "list schedules")
	}
	return ListResponse{Data: toResponses(views)}, nil
}

type GetByIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

// GetByID returns a single schedule. Non-owners may only see their own.
func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (ScheduleResponse, error) {
	user, err := callerContext(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.GetByID(ctx, user.ID, user.Role == middleware.RoleOwner, input.ID)
	if err != nil {
		return ScheduleResponse{}, h.mapErr(err, "load schedule")
	}
	return toResponse(*view), nil
}

// CreateInput captures the full schedule payload the dashboard posts. The
// CHECK constraints in the schema enforce that each has_X toggle has its
// corresponding value present; surfacing a tight 400 at the tRPC boundary
// keeps the UI simpler than wrapping driver-level constraint errors.
type CreateInput struct {
	BroadcasterID    string   `json:"broadcaster_id" validate:"required"`
	Quality          string   `json:"quality" validate:"required,oneof=LOW MEDIUM HIGH"`
	HasMinViewers    bool     `json:"has_min_viewers"`
	MinViewers       *int64   `json:"min_viewers,omitempty" validate:"omitempty,min=0"`
	HasCategories    bool     `json:"has_categories"`
	HasTags          bool     `json:"has_tags"`
	IsDeleteRediff   bool     `json:"is_delete_rediff"`
	TimeBeforeDelete *int64   `json:"time_before_delete,omitempty" validate:"omitempty,min=1"`
	IsDisabled       bool     `json:"is_disabled"`
	CategoryIDs      []string `json:"category_ids"`
	TagIDs           []int64  `json:"tag_ids"`
}

// Create registers a schedule for the caller. requested_by is always the
// caller; admins cannot create a schedule on someone else's behalf — role
// boundaries stay intact at the service layer.
func (h *Handler) Create(ctx context.Context, input CreateInput) (ScheduleResponse, error) {
	user, err := callerContext(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.Create(ctx, user.ID, schedulesvc.WriteInput{
		BroadcasterID:    input.BroadcasterID,
		Quality:          input.Quality,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       input.MinViewers,
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: input.TimeBeforeDelete,
		IsDisabled:       input.IsDisabled,
		CategoryIDs:      input.CategoryIDs,
		TagIDs:           input.TagIDs,
	})
	if err != nil {
		return ScheduleResponse{}, h.mapErr(err, "create schedule")
	}
	return toResponse(*view), nil
}

// UpdateInput mirrors CreateInput plus the schedule ID. We don't allow
// changing broadcaster_id — that would effectively move the schedule to
// another channel and should be a delete+create instead.
type UpdateInput struct {
	ID               int64    `json:"id" validate:"required"`
	Quality          string   `json:"quality" validate:"required,oneof=LOW MEDIUM HIGH"`
	HasMinViewers    bool     `json:"has_min_viewers"`
	MinViewers       *int64   `json:"min_viewers,omitempty" validate:"omitempty,min=0"`
	HasCategories    bool     `json:"has_categories"`
	HasTags          bool     `json:"has_tags"`
	IsDeleteRediff   bool     `json:"is_delete_rediff"`
	TimeBeforeDelete *int64   `json:"time_before_delete,omitempty" validate:"omitempty,min=1"`
	IsDisabled       bool     `json:"is_disabled"`
	CategoryIDs      []string `json:"category_ids"`
	TagIDs           []int64  `json:"tag_ids"`
}

// Update edits an existing schedule. Authors can edit their own; owners
// can edit any. The underlying SQL preserves trigger_count and
// last_triggered_at — see the adapter test for the regression gate.
func (h *Handler) Update(ctx context.Context, input UpdateInput) (ScheduleResponse, error) {
	user, err := callerContext(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.Update(ctx, user.ID, user.Role == middleware.RoleOwner, input.ID, schedulesvc.WriteInput{
		Quality:          input.Quality,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       input.MinViewers,
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: input.TimeBeforeDelete,
		IsDisabled:       input.IsDisabled,
		CategoryIDs:      input.CategoryIDs,
		TagIDs:           input.TagIDs,
	})
	if err != nil {
		return ScheduleResponse{}, h.mapErr(err, "update schedule")
	}
	return toResponse(*view), nil
}

type ToggleInput struct {
	ID int64 `json:"id" validate:"required"`
}

// Toggle flips is_disabled in one atomic UPDATE so the dashboard checkbox
// can POST without re-sending the full payload.
func (h *Handler) Toggle(ctx context.Context, input ToggleInput) (ScheduleResponse, error) {
	user, err := callerContext(ctx)
	if err != nil {
		return ScheduleResponse{}, err
	}
	view, err := h.svc.Toggle(ctx, user.ID, user.Role == middleware.RoleOwner, input.ID)
	if err != nil {
		return ScheduleResponse{}, h.mapErr(err, "toggle schedule")
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
	user, err := callerContext(ctx)
	if err != nil {
		return DeleteResponse{}, err
	}
	if err := h.svc.Delete(ctx, user.ID, user.Role == middleware.RoleOwner, input.ID); err != nil {
		return DeleteResponse{}, h.mapErr(err, "delete schedule")
	}
	return DeleteResponse{ID: input.ID}, nil
}

func toResponses(views []schedulesvc.View) []ScheduleResponse {
	out := make([]ScheduleResponse, 0, len(views))
	for _, v := range views {
		out = append(out, toResponse(v))
	}
	return out
}
