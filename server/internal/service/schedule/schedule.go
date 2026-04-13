// Package schedule owns the schedule-domain CRUD service plus the
// webhook-driven auto-download matcher and event processor.
//
// Two related but independent concerns live here:
//   - Service: schedule CRUD (authorization, filter validation,
//     category/tag junction replacement). Request-scoped; tRPC routes
//     call this.
//   - EventProcessor + Match: the webhook hot path. On stream.online
//     we enrich the event, run every active schedule through Match,
//     pick the highest-quality winner, and kick off exactly one
//     download. Long-lived; shares the repo + logger but has a
//     distinct lifecycle from the CRUD service.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// ErrNotOwner is returned when a non-owner tries to mutate a schedule
// they didn't create. The transport layer maps this to 403 — hiding
// it as 404 would complicate legitimate "did I really create that?"
// diagnostics for the author themselves. Role-level owners bypass
// this check.
var ErrNotOwner = errors.New("schedule: not your schedule")

// ErrInvalidFilter is returned when a has_X toggle is on but the
// associated value is missing or out of range. Mirrors the DB CHECK
// constraints so callers see a 400 at the boundary rather than a
// driver-level error deep in the write path.
var ErrInvalidFilter = errors.New("schedule: filter value missing for enabled toggle")

// Service owns schedule CRUD business logic. The tRPC route layer
// adapts DTOs <-> domain and applies role middleware.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the CRUD service. Logger is tagged with the scheduling
// domain so downstream slog attrs don't need to re-annotate.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "schedule")}
}

// View bundles a schedule row with its inlined category/tag
// junctions. The dashboard renders these per row, so the service
// inflates them once here rather than forcing N+1 at the transport.
type View struct {
	Schedule   *repository.DownloadSchedule
	Categories []repository.Category
	Tags       []repository.Tag
}

// WriteInput is the domain-shaped create/update payload. The route
// layer converts its DTO into this before calling the service so the
// service never sees JSON tags or tRPC-specific concerns.
type WriteInput struct {
	BroadcasterID    string
	Quality          string
	HasMinViewers    bool
	MinViewers       *int64
	HasCategories    bool
	HasTags          bool
	IsDeleteRediff   bool
	TimeBeforeDelete *int64
	IsDisabled       bool
	CategoryIDs      []string
	TagIDs           []int64
}

// List returns schedules visible to the caller. Owners see everything;
// everyone else sees only their own. The caller tells the service its
// role — we don't re-read the user row here.
func (s *Service) List(ctx context.Context, callerID string, callerIsOwner bool, limit, offset int) ([]View, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		rows []repository.DownloadSchedule
		err  error
	)
	if callerIsOwner {
		rows, err = s.repo.ListSchedules(ctx, limit, offset)
	} else {
		rows, err = s.repo.ListSchedulesForUser(ctx, callerID, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return s.inflateAll(ctx, rows)
}

// Mine returns schedules the caller created. Separate from List so the
// future public API can expose it to viewers without granting the
// system-wide read that owners get through List.
func (s *Service) Mine(ctx context.Context, callerID string, limit, offset int) ([]View, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.repo.ListSchedulesForUser(ctx, callerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list own schedules: %w", err)
	}
	return s.inflateAll(ctx, rows)
}

// GetByID loads and inflates a single schedule, enforcing that the
// caller is the owner-role user or the schedule's author. Returns
// repository.ErrNotFound for missing rows and ErrNotOwner for
// visibility violations — the transport layer distinguishes these for
// correct HTTP status.
func (s *Service) GetByID(ctx context.Context, callerID string, callerIsOwner bool, id int64) (*View, error) {
	sched, err := s.repo.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	if !callerIsOwner && sched.RequestedBy != callerID {
		return nil, ErrNotOwner
	}
	return s.inflateOne(ctx, sched)
}

// Create registers a schedule for the caller. BroadcasterID can't be
// changed later — UpdateSchedule preserves it — so input validation
// blocks a malformed create up front.
func (s *Service) Create(ctx context.Context, callerID string, input WriteInput) (*View, error) {
	if err := validateFilterConsistency(input.HasMinViewers, input.MinViewers, input.IsDeleteRediff, input.TimeBeforeDelete); err != nil {
		return nil, err
	}
	sched, err := s.repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID:    input.BroadcasterID,
		RequestedBy:      callerID,
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
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	if err := s.replaceJunctions(ctx, sched.ID, input.CategoryIDs, input.TagIDs); err != nil {
		return nil, err
	}
	return s.inflateOne(ctx, sched)
}

// Update edits an existing schedule. Preserves broadcaster_id and
// requested_by from the stored row — a change to either would move
// schedule ownership, which we forbid. Category/tag sets get replaced
// to match the input.
func (s *Service) Update(ctx context.Context, callerID string, callerIsOwner bool, id int64, input WriteInput) (*View, error) {
	existing, err := s.repo.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	if !callerIsOwner && existing.RequestedBy != callerID {
		return nil, ErrNotOwner
	}
	if err := validateFilterConsistency(input.HasMinViewers, input.MinViewers, input.IsDeleteRediff, input.TimeBeforeDelete); err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdateSchedule(ctx, id, &repository.ScheduleInput{
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
		return nil, fmt.Errorf("update schedule: %w", err)
	}
	if err := s.replaceJunctions(ctx, updated.ID, input.CategoryIDs, input.TagIDs); err != nil {
		return nil, err
	}
	return s.inflateOne(ctx, updated)
}

// Toggle flips is_disabled in one write. The dashboard's enable/disable
// checkbox shouldn't have to roundtrip the whole schedule payload.
func (s *Service) Toggle(ctx context.Context, callerID string, callerIsOwner bool, id int64) (*View, error) {
	existing, err := s.repo.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	if !callerIsOwner && existing.RequestedBy != callerID {
		return nil, ErrNotOwner
	}
	toggled, err := s.repo.ToggleSchedule(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("toggle schedule: %w", err)
	}
	return s.inflateOne(ctx, toggled)
}

// Delete removes the schedule and cascades to its junction rows via FK.
func (s *Service) Delete(ctx context.Context, callerID string, callerIsOwner bool, id int64) error {
	existing, err := s.repo.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if !callerIsOwner && existing.RequestedBy != callerID {
		return ErrNotOwner
	}
	if err := s.repo.DeleteSchedule(ctx, id); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

func (s *Service) inflateOne(ctx context.Context, sched *repository.DownloadSchedule) (*View, error) {
	cats, err := s.repo.ListScheduleCategories(ctx, sched.ID)
	if err != nil {
		return nil, fmt.Errorf("inflate categories: %w", err)
	}
	tags, err := s.repo.ListScheduleTags(ctx, sched.ID)
	if err != nil {
		return nil, fmt.Errorf("inflate tags: %w", err)
	}
	return &View{Schedule: sched, Categories: cats, Tags: tags}, nil
}

func (s *Service) inflateAll(ctx context.Context, rows []repository.DownloadSchedule) ([]View, error) {
	out := make([]View, 0, len(rows))
	for i := range rows {
		v, err := s.inflateOne(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// replaceJunctions runs the "set replace" pattern for categories and
// tags — clear then re-link. Atomic-enough for our scale (one schedule
// rarely has concurrent writers). If this becomes hot, lift into a
// transaction on the adapter.
func (s *Service) replaceJunctions(ctx context.Context, scheduleID int64, categoryIDs []string, tagIDs []int64) error {
	if err := s.repo.ClearScheduleCategories(ctx, scheduleID); err != nil {
		return fmt.Errorf("clear categories: %w", err)
	}
	for _, id := range categoryIDs {
		if err := s.repo.LinkScheduleCategory(ctx, scheduleID, id); err != nil {
			return fmt.Errorf("link category %s: %w", id, err)
		}
	}
	if err := s.repo.ClearScheduleTags(ctx, scheduleID); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}
	for _, id := range tagIDs {
		if err := s.repo.LinkScheduleTag(ctx, scheduleID, id); err != nil {
			return fmt.Errorf("link tag %d: %w", id, err)
		}
	}
	return nil
}

// validateFilterConsistency mirrors the schema CHECK constraints. Keeps
// the tRPC 400 close to the user's input instead of surfacing a driver
// error 50 layers down the write path.
func validateFilterConsistency(hasMinViewers bool, minViewers *int64, isDeleteRediff bool, timeBeforeDelete *int64) error {
	if hasMinViewers && (minViewers == nil || *minViewers < 0) {
		return fmt.Errorf("%w: has_min_viewers=true requires min_viewers >= 0", ErrInvalidFilter)
	}
	if isDeleteRediff && (timeBeforeDelete == nil || *timeBeforeDelete <= 0) {
		return fmt.Errorf("%w: is_delete_rediff=true requires time_before_delete > 0", ErrInvalidFilter)
	}
	return nil
}
