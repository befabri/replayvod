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
	"slices"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

const immediateLiveTriggerTimeout = 10 * time.Second

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
	repo        repository.Repository
	log         *slog.Logger
	liveTrigger LiveTrigger
}

type Option func(*Service)

func WithImmediateLiveTrigger(trigger LiveTrigger) Option {
	return func(s *Service) {
		s.liveTrigger = trigger
	}
}

// New builds the CRUD service. Logger is tagged with the scheduling domain so
// downstream slog attrs don't need to re-annotate. Optional hooks are expressed
// as interfaces so schedule writes do not depend on Twitch or downloader
// concrete types.
func New(repo repository.Repository, log *slog.Logger, opts ...Option) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{repo: repo, log: log.With("domain", "schedule")}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
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
	RecordingType    string
	Quality          string
	ForceH264        *bool
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

// readSchedulesPaused reports the global auto-download pause flag, treating a
// missing settings row (fresh DB before the first owner save) as not paused.
// Shared by the CRUD service (PausedState) and the webhook processor.
func readSchedulesPaused(ctx context.Context, repo repository.Repository) (bool, error) {
	settings, err := repo.GetServerSettings(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return settings.SchedulesPaused, nil
}

// PausedState reports whether auto-downloads are globally paused.
func (s *Service) PausedState(ctx context.Context) (bool, error) {
	return readSchedulesPaused(ctx, s.repo)
}

// SetPaused flips the global auto-download pause flag and returns the persisted
// value. Individual schedule is_disabled state is never touched, so resuming
// restores each schedule's prior behavior exactly.
func (s *Service) SetPaused(ctx context.Context, paused bool) (bool, error) {
	settings, err := s.repo.SetSchedulesPaused(ctx, paused)
	if err != nil {
		return false, err
	}
	return settings.SchedulesPaused, nil
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
	if err := validateFilterConsistency(input); err != nil {
		return nil, err
	}
	recordingSettings := normalizeRecordingSettings(input.RecordingType, input.Quality, input.ForceH264, nil)
	sched, err := s.repo.CreateScheduleWithFilters(ctx, &repository.ScheduleInput{
		BroadcasterID:    input.BroadcasterID,
		RequestedBy:      callerID,
		RecordingType:    recordingSettings.RecordingType,
		Quality:          recordingSettings.Quality,
		ForceH264:        recordingSettings.ForceH264,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       input.MinViewers,
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: input.TimeBeforeDelete,
		IsDisabled:       input.IsDisabled,
	}, repository.ScheduleFilterInput{
		CategoryIDs: input.CategoryIDs,
		TagIDs:      input.TagIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	sched = s.triggerLiveIfEligible(ctx, sched)
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
	if err := validateFilterConsistency(input); err != nil {
		return nil, err
	}
	shouldTrigger, err := s.shouldTriggerLiveAfterUpdate(ctx, existing, input)
	if err != nil {
		return nil, err
	}
	recordingSettings := normalizeRecordingSettings(input.RecordingType, input.Quality, input.ForceH264, existing)
	updated, err := s.repo.UpdateScheduleWithFilters(ctx, id, &repository.ScheduleInput{
		BroadcasterID:    existing.BroadcasterID,
		RequestedBy:      existing.RequestedBy,
		RecordingType:    recordingSettings.RecordingType,
		Quality:          recordingSettings.Quality,
		ForceH264:        recordingSettings.ForceH264,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       input.MinViewers,
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: input.TimeBeforeDelete,
		IsDisabled:       input.IsDisabled,
	}, repository.ScheduleFilterInput{
		CategoryIDs: input.CategoryIDs,
		TagIDs:      input.TagIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("update schedule: %w", err)
	}
	if shouldTrigger {
		updated = s.triggerLiveIfEligible(ctx, updated)
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
	toggled = s.triggerLiveIfEligible(ctx, toggled)
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

func (s *Service) triggerLiveIfEligible(ctx context.Context, sched *repository.DownloadSchedule) *repository.DownloadSchedule {
	if s.liveTrigger == nil || sched == nil || sched.IsDisabled {
		return sched
	}

	persistCtx := context.WithoutCancel(ctx)
	triggerCtx, cancel := context.WithTimeout(persistCtx, immediateLiveTriggerTimeout)
	err := s.liveTrigger.TriggerScheduleIfLive(triggerCtx, sched.ID, sched.BroadcasterID)
	cancel()
	if err != nil {
		s.log.Warn("immediate schedule live trigger failed",
			"schedule_id", sched.ID,
			"broadcaster_id", sched.BroadcasterID,
			"error", err)
	}

	refreshed, refreshErr := s.repo.GetSchedule(persistCtx, sched.ID)
	if refreshErr != nil {
		s.log.Warn("reload schedule after immediate live trigger",
			"schedule_id", sched.ID,
			"error", refreshErr)
		return sched
	}
	return refreshed
}

func (s *Service) shouldTriggerLiveAfterUpdate(ctx context.Context, existing *repository.DownloadSchedule, input WriteInput) (bool, error) {
	if existing == nil || input.IsDisabled {
		return false, nil
	}
	if existing.IsDisabled {
		return true, nil
	}
	before, err := s.activationSignatureForSchedule(ctx, existing)
	if err != nil {
		return false, err
	}
	after := activationSignatureFromInput(input)
	return !before.equal(after), nil
}

type activationSignature struct {
	hasMinViewers bool
	minViewers    int64
	hasCategories bool
	categoryIDs   []string
	hasTags       bool
	tagIDs        []int64
}

func (s *Service) activationSignatureForSchedule(ctx context.Context, schedule *repository.DownloadSchedule) (activationSignature, error) {
	out := activationSignature{
		hasMinViewers: schedule.HasMinViewers,
		hasCategories: schedule.HasCategories,
		hasTags:       schedule.HasTags,
	}
	if schedule.HasMinViewers && schedule.MinViewers != nil {
		out.minViewers = *schedule.MinViewers
	}
	if schedule.HasCategories {
		cats, err := s.repo.ListScheduleCategories(ctx, schedule.ID)
		if err != nil {
			return out, fmt.Errorf("load existing schedule categories: %w", err)
		}
		ids := make([]string, 0, len(cats))
		for _, c := range cats {
			ids = append(ids, c.ID)
		}
		out.categoryIDs = canonicalStringIDs(ids)
	}
	if schedule.HasTags {
		tags, err := s.repo.ListScheduleTags(ctx, schedule.ID)
		if err != nil {
			return out, fmt.Errorf("load existing schedule tags: %w", err)
		}
		ids := make([]int64, 0, len(tags))
		for _, t := range tags {
			ids = append(ids, t.ID)
		}
		out.tagIDs = canonicalInt64IDs(ids)
	}
	return out, nil
}

func activationSignatureFromInput(input WriteInput) activationSignature {
	out := activationSignature{
		hasMinViewers: input.HasMinViewers,
		hasCategories: input.HasCategories,
		hasTags:       input.HasTags,
	}
	if input.HasMinViewers && input.MinViewers != nil {
		out.minViewers = *input.MinViewers
	}
	if input.HasCategories {
		out.categoryIDs = canonicalStringIDs(input.CategoryIDs)
	}
	if input.HasTags {
		out.tagIDs = canonicalInt64IDs(input.TagIDs)
	}
	return out
}

func (s activationSignature) equal(other activationSignature) bool {
	return s.hasMinViewers == other.hasMinViewers &&
		s.minViewers == other.minViewers &&
		s.hasCategories == other.hasCategories &&
		slices.Equal(s.categoryIDs, other.categoryIDs) &&
		s.hasTags == other.hasTags &&
		slices.Equal(s.tagIDs, other.tagIDs)
}

func canonicalStringIDs(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

func canonicalInt64IDs(ids []int64) []int64 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// validateFilterConsistency mirrors the schedule filter invariants. Some are
// DB CHECK constraints (min viewers, retention window), while set-valued
// category/tag filters live in junction tables and need service-level guards to
// avoid enabled filters with no possible match.
func validateFilterConsistency(input WriteInput) error {
	if input.HasMinViewers && (input.MinViewers == nil || *input.MinViewers < 0) {
		return fmt.Errorf("%w: has_min_viewers=true requires min_viewers >= 0", ErrInvalidFilter)
	}
	if input.HasCategories {
		if len(input.CategoryIDs) == 0 {
			return fmt.Errorf("%w: has_categories=true requires at least one category_id", ErrInvalidFilter)
		}
		for _, id := range input.CategoryIDs {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("%w: category_ids cannot contain empty IDs", ErrInvalidFilter)
			}
		}
	}
	if input.HasTags {
		if len(input.TagIDs) == 0 {
			return fmt.Errorf("%w: has_tags=true requires at least one tag_id", ErrInvalidFilter)
		}
		for _, id := range input.TagIDs {
			if id <= 0 {
				return fmt.Errorf("%w: tag_ids must be positive", ErrInvalidFilter)
			}
		}
	}
	if input.IsDeleteRediff && (input.TimeBeforeDelete == nil || *input.TimeBeforeDelete <= 0) {
		return fmt.Errorf("%w: is_delete_rediff=true requires time_before_delete > 0", ErrInvalidFilter)
	}
	// Bound the window only when delete is enabled, matching the schema CHECK
	// (which gates the ceiling on is_delete_rediff). A non-delete schedule's
	// stale time_before_delete is never read, so rejecting it would be stricter
	// than the DB.
	if input.IsDeleteRediff && input.TimeBeforeDelete != nil && *input.TimeBeforeDelete > repository.MaxRetentionWindowHours {
		return fmt.Errorf("%w: time_before_delete must be <= %d hours", ErrInvalidFilter, repository.MaxRetentionWindowHours)
	}
	return nil
}

type recordingSettings struct {
	RecordingType string
	Quality       string
	ForceH264     bool
}

func normalizeRecordingSettings(recordingType string, quality string, forceH264 *bool, existing *repository.DownloadSchedule) recordingSettings {
	if recordingType == "" && existing != nil {
		recordingType = existing.RecordingType
	}
	input := repository.RecordingSettingsInput{
		RecordingType: recordingType,
		Quality:       repository.QualityHigh,
	}
	if existing != nil {
		input.Quality = existing.Quality
		input.ForceH264 = existing.ForceH264
	}
	if quality != "" {
		input.Quality = quality
	}
	if forceH264 != nil {
		input.ForceH264 = *forceH264
	}
	normalized := repository.NormalizeRecordingSettings(input)
	return recordingSettings(normalized)
}
