// Package schedule implements the schedule.* tRPC procedures. Admins
// manage their own schedules; owners see everyone's. Auto-download
// triggers (the actual pipeline firing) are wired through the webhook
// processor — this package handles only CRUD + category/tag junctions.
package schedule

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC schedule procedures.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService creates a new schedule tRPC service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "schedule"),
	}
}

// ScheduleResponse is the wire shape the dashboard consumes. Categories
// and tags are inlined so the list page doesn't have to N+1 per row.
type ScheduleResponse struct {
	ID               int64             `json:"id"`
	BroadcasterID    string            `json:"broadcaster_id"`
	RequestedBy      string            `json:"requested_by"`
	Quality          string            `json:"quality"`
	HasMinViewers    bool              `json:"has_min_viewers"`
	MinViewers       *int64            `json:"min_viewers,omitempty"`
	HasCategories    bool              `json:"has_categories"`
	HasTags          bool              `json:"has_tags"`
	IsDeleteRediff   bool              `json:"is_delete_rediff"`
	TimeBeforeDelete *int64            `json:"time_before_delete,omitempty"`
	IsDisabled       bool              `json:"is_disabled"`
	LastTriggeredAt  *time.Time        `json:"last_triggered_at,omitempty"`
	TriggerCount     int64             `json:"trigger_count"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	Categories       []CategoryLink    `json:"categories"`
	Tags             []TagLink         `json:"tags"`
}

type CategoryLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TagLink struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// toResponse inflates a schedule row + its category/tag junctions into
// the wire shape.
func (s *Service) toResponse(ctx context.Context, sched *repository.DownloadSchedule) (ScheduleResponse, error) {
	resp := ScheduleResponse{
		ID:               sched.ID,
		BroadcasterID:    sched.BroadcasterID,
		RequestedBy:      sched.RequestedBy,
		Quality:          sched.Quality,
		HasMinViewers:    sched.HasMinViewers,
		MinViewers:       sched.MinViewers,
		HasCategories:    sched.HasCategories,
		HasTags:          sched.HasTags,
		IsDeleteRediff:   sched.IsDeleteRediff,
		TimeBeforeDelete: sched.TimeBeforeDelete,
		IsDisabled:       sched.IsDisabled,
		LastTriggeredAt:  sched.LastTriggeredAt,
		TriggerCount:     sched.TriggerCount,
		CreatedAt:        sched.CreatedAt,
		UpdatedAt:        sched.UpdatedAt,
		Categories:       []CategoryLink{},
		Tags:             []TagLink{},
	}
	cats, err := s.repo.ListScheduleCategories(ctx, sched.ID)
	if err != nil {
		return resp, err
	}
	for _, c := range cats {
		resp.Categories = append(resp.Categories, CategoryLink{ID: c.ID, Name: c.Name})
	}
	tags, err := s.repo.ListScheduleTags(ctx, sched.ID)
	if err != nil {
		return resp, err
	}
	for _, t := range tags {
		resp.Tags = append(resp.Tags, TagLink{ID: t.ID, Name: t.Name})
	}
	return resp, nil
}

// ensureOwnerOrAuthor returns an error when the caller isn't the schedule
// owner and isn't the system owner. Admin-tier operations (update, delete,
// toggle) use this to prevent admin A from clobbering admin B's schedule.
func (s *Service) ensureOwnerOrAuthor(user *repository.User, sched *repository.DownloadSchedule) error {
	if user == nil {
		return trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if user.Role == middleware.RoleOwner {
		return nil
	}
	if sched.RequestedBy != user.ID {
		return trpcgo.NewError(trpcgo.CodeForbidden, "not your schedule")
	}
	return nil
}

// --- Procedures ---

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
func (s *Service) List(ctx context.Context, input ListInput) (ListResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return ListResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}

	var schedules []repository.DownloadSchedule
	var err error
	if user.Role == middleware.RoleOwner {
		schedules, err = s.repo.ListSchedules(ctx, limit, input.Offset)
	} else {
		schedules, err = s.repo.ListSchedulesForUser(ctx, user.ID, limit, input.Offset)
	}
	if err != nil {
		s.log.Error("list schedules", "error", err)
		return ListResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list schedules")
	}

	data := make([]ScheduleResponse, 0, len(schedules))
	for i := range schedules {
		resp, err := s.toResponse(ctx, &schedules[i])
		if err != nil {
			s.log.Error("inflate schedule", "id", schedules[i].ID, "error", err)
			return ListResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list schedules")
		}
		data = append(data, resp)
	}
	return ListResponse{Data: data}, nil
}

// Mine returns schedules owned by the calling user. Viewers use this to
// see what they've set up even if (for a future public-facing API) they
// can't see the system-wide list.
func (s *Service) Mine(ctx context.Context, input ListInput) (ListResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return ListResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	schedules, err := s.repo.ListSchedulesForUser(ctx, user.ID, limit, input.Offset)
	if err != nil {
		s.log.Error("list user schedules", "error", err)
		return ListResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list schedules")
	}
	data := make([]ScheduleResponse, 0, len(schedules))
	for i := range schedules {
		resp, err := s.toResponse(ctx, &schedules[i])
		if err != nil {
			return ListResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list schedules")
		}
		data = append(data, resp)
	}
	return ListResponse{Data: data}, nil
}

type GetByIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

// GetByID returns a single schedule. Non-owners may only see their own.
func (s *Service) GetByID(ctx context.Context, input GetByIDInput) (ScheduleResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	sched, err := s.repo.GetSchedule(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "schedule not found")
		}
		s.log.Error("get schedule", "error", err)
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load schedule")
	}
	if err := s.ensureOwnerOrAuthor(user, sched); err != nil {
		return ScheduleResponse{}, err
	}
	return s.toResponseOrError(ctx, sched)
}

func (s *Service) toResponseOrError(ctx context.Context, sched *repository.DownloadSchedule) (ScheduleResponse, error) {
	resp, err := s.toResponse(ctx, sched)
	if err != nil {
		s.log.Error("inflate schedule", "id", sched.ID, "error", err)
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load schedule")
	}
	return resp, nil
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
// boundaries stay intact.
func (s *Service) Create(ctx context.Context, input CreateInput) (ScheduleResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if err := validateFilterConsistency(input.HasMinViewers, input.MinViewers, input.IsDeleteRediff, input.TimeBeforeDelete); err != nil {
		return ScheduleResponse{}, err
	}

	sched, err := s.repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID:    input.BroadcasterID,
		RequestedBy:      user.ID,
		Quality:          input.Quality,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       input.MinViewers,
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: input.TimeBeforeDelete,
		IsDisabled:       input.IsDisabled,
	})
	if err != nil {
		s.log.Error("create schedule", "error", err)
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to create schedule")
	}

	if err := s.replaceCategories(ctx, sched.ID, input.CategoryIDs); err != nil {
		return ScheduleResponse{}, err
	}
	if err := s.replaceTags(ctx, sched.ID, input.TagIDs); err != nil {
		return ScheduleResponse{}, err
	}
	return s.toResponseOrError(ctx, sched)
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
func (s *Service) Update(ctx context.Context, input UpdateInput) (ScheduleResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	existing, err := s.repo.GetSchedule(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "schedule not found")
		}
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load schedule")
	}
	if err := s.ensureOwnerOrAuthor(user, existing); err != nil {
		return ScheduleResponse{}, err
	}
	if err := validateFilterConsistency(input.HasMinViewers, input.MinViewers, input.IsDeleteRediff, input.TimeBeforeDelete); err != nil {
		return ScheduleResponse{}, err
	}

	updated, err := s.repo.UpdateSchedule(ctx, input.ID, &repository.ScheduleInput{
		BroadcasterID:    existing.BroadcasterID,
		RequestedBy:      existing.RequestedBy,
		Quality:          input.Quality,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       input.MinViewers,
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: input.TimeBeforeDelete,
		IsDisabled:       input.IsDisabled,
	})
	if err != nil {
		s.log.Error("update schedule", "error", err)
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to update schedule")
	}

	if err := s.replaceCategories(ctx, updated.ID, input.CategoryIDs); err != nil {
		return ScheduleResponse{}, err
	}
	if err := s.replaceTags(ctx, updated.ID, input.TagIDs); err != nil {
		return ScheduleResponse{}, err
	}
	return s.toResponseOrError(ctx, updated)
}

type ToggleInput struct {
	ID int64 `json:"id" validate:"required"`
}

// Toggle flips is_disabled in one atomic UPDATE so the dashboard checkbox
// can POST without re-sending the full payload.
func (s *Service) Toggle(ctx context.Context, input ToggleInput) (ScheduleResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	existing, err := s.repo.GetSchedule(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "schedule not found")
		}
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load schedule")
	}
	if err := s.ensureOwnerOrAuthor(user, existing); err != nil {
		return ScheduleResponse{}, err
	}
	toggled, err := s.repo.ToggleSchedule(ctx, input.ID)
	if err != nil {
		s.log.Error("toggle schedule", "error", err)
		return ScheduleResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to toggle schedule")
	}
	return s.toResponseOrError(ctx, toggled)
}

type DeleteInput struct {
	ID int64 `json:"id" validate:"required"`
}

type DeleteResponse struct {
	ID int64 `json:"id"`
}

// Delete removes a schedule and its junction rows (ON DELETE CASCADE).
func (s *Service) Delete(ctx context.Context, input DeleteInput) (DeleteResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return DeleteResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	existing, err := s.repo.GetSchedule(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return DeleteResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "schedule not found")
		}
		return DeleteResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load schedule")
	}
	if err := s.ensureOwnerOrAuthor(user, existing); err != nil {
		return DeleteResponse{}, err
	}
	if err := s.repo.DeleteSchedule(ctx, input.ID); err != nil {
		s.log.Error("delete schedule", "error", err)
		return DeleteResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to delete schedule")
	}
	return DeleteResponse{ID: input.ID}, nil
}

// replaceCategories is the "set" pattern: clear existing links, re-link to
// the provided set. Atomic-enough for our scale (rare edit, no concurrent
// writers for a single schedule); a future move to transactional replace
// would be the next step if it becomes hot.
func (s *Service) replaceCategories(ctx context.Context, scheduleID int64, categoryIDs []string) error {
	if err := s.repo.ClearScheduleCategories(ctx, scheduleID); err != nil {
		s.log.Error("clear categories", "id", scheduleID, "error", err)
		return trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to update categories")
	}
	for _, id := range categoryIDs {
		if err := s.repo.LinkScheduleCategory(ctx, scheduleID, id); err != nil {
			s.log.Error("link category", "schedule", scheduleID, "category", id, "error", err)
			return trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to link category")
		}
	}
	return nil
}

func (s *Service) replaceTags(ctx context.Context, scheduleID int64, tagIDs []int64) error {
	if err := s.repo.ClearScheduleTags(ctx, scheduleID); err != nil {
		s.log.Error("clear tags", "id", scheduleID, "error", err)
		return trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to update tags")
	}
	for _, id := range tagIDs {
		if err := s.repo.LinkScheduleTag(ctx, scheduleID, id); err != nil {
			s.log.Error("link tag", "schedule", scheduleID, "tag", id, "error", err)
			return trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to link tag")
		}
	}
	return nil
}

// validateFilterConsistency mirrors the schema CHECK constraints so bad
// input gets a clean 400 at the tRPC boundary rather than a driver error
// 50 layers down.
func validateFilterConsistency(hasMinViewers bool, minViewers *int64, isDeleteRediff bool, timeBeforeDelete *int64) error {
	if hasMinViewers && (minViewers == nil || *minViewers < 0) {
		return trpcgo.NewError(trpcgo.CodeBadRequest, "has_min_viewers=true requires min_viewers >= 0")
	}
	if isDeleteRediff && (timeBeforeDelete == nil || *timeBeforeDelete <= 0) {
		return trpcgo.NewError(trpcgo.CodeBadRequest, "is_delete_rediff=true requires time_before_delete > 0")
	}
	return nil
}
