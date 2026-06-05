package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestService_PausedState_RoundTrip pins the global pause flag through the
// service: it defaults to false on a fresh DB, SetPaused persists the new value
// and echoes it back, and PausedState reads it back.
func TestService_PausedState_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(repo, log)

	if paused, err := svc.PausedState(ctx); err != nil {
		t.Fatalf("PausedState (fresh) = %v", err)
	} else if paused {
		t.Fatal("PausedState must default to false")
	}

	if paused, err := svc.SetPaused(ctx, true); err != nil {
		t.Fatalf("SetPaused(true) = %v", err)
	} else if !paused {
		t.Fatal("SetPaused(true) must return true")
	}
	if paused, err := svc.PausedState(ctx); err != nil || !paused {
		t.Fatalf("PausedState after pause = (%v, %v), want (true, nil)", paused, err)
	}

	if paused, err := svc.SetPaused(ctx, false); err != nil || paused {
		t.Fatalf("SetPaused(false) = (%v, %v), want (false, nil)", paused, err)
	}
}

type fakeLiveTrigger struct {
	calls  []liveTriggerCall
	err    error
	onCall func(context.Context, int64, string)
}

type liveTriggerCall struct {
	scheduleID    int64
	broadcasterID string
}

func (f *fakeLiveTrigger) TriggerScheduleIfLive(ctx context.Context, scheduleID int64, broadcasterID string) error {
	f.calls = append(f.calls, liveTriggerCall{scheduleID: scheduleID, broadcasterID: broadcasterID})
	if f.onCall != nil {
		f.onCall(ctx, scheduleID, broadcasterID)
	}
	return f.err
}

type scheduleRepoFailure struct {
	repository.Repository
	listErr              error
	listForUserErr       error
	categoriesErr        error
	categoriesScheduleID int64
	tagsErr              error
	tagsScheduleID       int64
	deleteErr            error
	deleteScheduleID     int64
}

func (r *scheduleRepoFailure) ListSchedules(ctx context.Context, limit, offset int) ([]repository.DownloadSchedule, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.Repository.ListSchedules(ctx, limit, offset)
}

func (r *scheduleRepoFailure) ListSchedulesForUser(ctx context.Context, userID string, limit, offset int) ([]repository.DownloadSchedule, error) {
	if r.listForUserErr != nil {
		return nil, r.listForUserErr
	}
	return r.Repository.ListSchedulesForUser(ctx, userID, limit, offset)
}

func (r *scheduleRepoFailure) ListScheduleCategories(ctx context.Context, scheduleID int64) ([]repository.Category, error) {
	if r.categoriesErr != nil && scheduleID == r.categoriesScheduleID {
		return nil, r.categoriesErr
	}
	return r.Repository.ListScheduleCategories(ctx, scheduleID)
}

func (r *scheduleRepoFailure) ListScheduleTags(ctx context.Context, scheduleID int64) ([]repository.Tag, error) {
	if r.tagsErr != nil && scheduleID == r.tagsScheduleID {
		return nil, r.tagsErr
	}
	return r.Repository.ListScheduleTags(ctx, scheduleID)
}

func (r *scheduleRepoFailure) DeleteSchedule(ctx context.Context, scheduleID int64) error {
	if r.deleteErr != nil && scheduleID == r.deleteScheduleID {
		return r.deleteErr
	}
	return r.Repository.DeleteSchedule(ctx, scheduleID)
}

func TestValidateFilterConsistencyRejectsRetentionWindowOverflow(t *testing.T) {
	tooLarge := repository.MaxRetentionWindowHours + 1
	err := validateFilterConsistency(WriteInput{IsDeleteRediff: true, TimeBeforeDelete: &tooLarge})
	if !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("validateFilterConsistency err = %v, want ErrInvalidFilter", err)
	}
}

// TestValidateFilterConsistencyAllowsStaleWindowWhenDeleteOff pins that the
// bound is gated on is_delete_rediff, matching the DB CHECK: when delete is off,
// time_before_delete is a dead field, so a stale over-ceiling value is allowed
// rather than rejected. The API handler no longer carries an unconditional tag
// that would contradict this.
func TestValidateFilterConsistencyAllowsStaleWindowWhenDeleteOff(t *testing.T) {
	tooLarge := repository.MaxRetentionWindowHours + 1
	if err := validateFilterConsistency(WriteInput{TimeBeforeDelete: &tooLarge}); err != nil {
		t.Fatalf("validateFilterConsistency(deleteOff, staleWindow) = %v, want nil", err)
	}
}

func TestValidateFilterConsistencyRejectsEnabledEmptyCategoryAndTagFilters(t *testing.T) {
	cases := []struct {
		name  string
		input WriteInput
	}{
		{
			name:  "categories enabled without IDs",
			input: WriteInput{HasCategories: true},
		},
		{
			name:  "categories enabled with blank ID",
			input: WriteInput{HasCategories: true, CategoryIDs: []string{"game-1", " "}},
		},
		{
			name:  "tags enabled without IDs",
			input: WriteInput{HasTags: true},
		},
		{
			name:  "tags enabled with non-positive ID",
			input: WriteInput{HasTags: true, TagIDs: []int64{0}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateFilterConsistency(tc.input); !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("validateFilterConsistency err = %v, want ErrInvalidFilter", err)
			}
		})
	}
}

func TestNormalizeRecordingSettings(t *testing.T) {
	cases := []struct {
		name          string
		recordingType string
		quality       string
		forceH264     *bool
		existing      *repository.DownloadSchedule
		wantType      string
		wantQuality   string
		wantForce     bool
	}{
		{
			name:        "empty create defaults video high",
			wantType:    repository.RecordingTypeVideo,
			wantQuality: repository.QualityHigh,
			wantForce:   false,
		},
		{
			name:          "video can force h264",
			recordingType: repository.RecordingTypeVideo,
			quality:       repository.QualityLow,
			forceH264:     boolPtr(true),
			wantType:      repository.RecordingTypeVideo,
			wantQuality:   repository.QualityLow,
			wantForce:     true,
		},
		{
			name:          "audio clears h264",
			recordingType: repository.RecordingTypeAudio,
			forceH264:     boolPtr(true),
			wantType:      repository.RecordingTypeAudio,
			wantQuality:   repository.QualityHigh,
			wantForce:     false,
		},
		{
			name:        "update preserves existing mode quality and h264 when omitted",
			existing:    &repository.DownloadSchedule{RecordingType: repository.RecordingTypeVideo, Quality: repository.QualityMedium, ForceH264: true},
			wantType:    repository.RecordingTypeVideo,
			wantQuality: repository.QualityMedium,
			wantForce:   true,
		},
		{
			name:          "switching existing video to audio clears h264",
			recordingType: repository.RecordingTypeAudio,
			existing:      &repository.DownloadSchedule{RecordingType: repository.RecordingTypeVideo, Quality: repository.QualityMedium, ForceH264: true},
			wantType:      repository.RecordingTypeAudio,
			wantQuality:   repository.QualityMedium,
			wantForce:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRecordingSettings(tc.recordingType, tc.quality, tc.forceH264, tc.existing)
			if got.RecordingType != tc.wantType || got.Quality != tc.wantQuality || got.ForceH264 != tc.wantForce {
				t.Fatalf("normalizeRecordingSettings() = %+v, want type=%q quality=%q force=%v", got, tc.wantType, tc.wantQuality, tc.wantForce)
			}
		})
	}
}

func TestCreate_TriggersLiveAfterJunctionsAndReturnsRefreshedSchedule(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-1")
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-1", Name: "Game 1"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	tag, err := repo.UpsertTag(ctx, "tag-one")
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	var sawLinkedFilters bool
	trigger := &fakeLiveTrigger{
		onCall: func(callCtx context.Context, scheduleID int64, broadcasterID string) {
			cats, err := repo.ListScheduleCategories(callCtx, scheduleID)
			if err != nil {
				t.Fatalf("ListScheduleCategories during trigger: %v", err)
			}
			tags, err := repo.ListScheduleTags(callCtx, scheduleID)
			if err != nil {
				t.Fatalf("ListScheduleTags during trigger: %v", err)
			}
			sawLinkedFilters = len(cats) == 1 && cats[0].ID == "game-1" && len(tags) == 1 && tags[0].ID == tag.ID
			if err := repo.RecordScheduleTrigger(callCtx, scheduleID); err != nil {
				t.Fatalf("RecordScheduleTrigger during trigger: %v", err)
			}
		},
	}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	view, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID:  "b-1",
		RecordingType:  repository.RecordingTypeVideo,
		Quality:        repository.QualityHigh,
		HasCategories:  true,
		HasTags:        true,
		CategoryIDs:    []string{"game-1"},
		TagIDs:         []int64{tag.ID},
		IsDeleteRediff: false,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(trigger.calls) != 1 {
		t.Fatalf("trigger calls = %d, want 1", len(trigger.calls))
	}
	if trigger.calls[0].broadcasterID != "b-1" || trigger.calls[0].scheduleID != view.Schedule.ID {
		t.Fatalf("trigger call = %+v, want created schedule for b-1", trigger.calls[0])
	}
	if !sawLinkedFilters {
		t.Fatal("trigger ran before schedule category/tag links were visible")
	}
	if view.Schedule.TriggerCount != 1 || view.Schedule.LastTriggeredAt == nil {
		t.Fatalf("returned schedule trigger metadata = count %d at %v, want refreshed count=1 with timestamp",
			view.Schedule.TriggerCount, view.Schedule.LastTriggeredAt)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestCreate_DisabledScheduleDoesNotTriggerLive(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-disabled")
	trigger := &fakeLiveTrigger{}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	if _, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-disabled",
		Quality:       repository.QualityHigh,
		IsDisabled:    true,
	}); err != nil {
		t.Fatalf("Create disabled: %v", err)
	}
	if len(trigger.calls) != 0 {
		t.Fatalf("disabled create trigger calls = %d, want 0", len(trigger.calls))
	}
}

func TestCreate_RollsBackScheduleWhenFilterLinkFails(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-rollback")
	trigger := &fakeLiveTrigger{}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	if _, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-rollback",
		Quality:       repository.QualityHigh,
		HasTags:       true,
		TagIDs:        []int64{999_999},
	}); err == nil {
		t.Fatal("Create with missing tag succeeded, want FK error")
	}
	if len(trigger.calls) != 0 {
		t.Fatalf("trigger calls after failed create = %d, want 0", len(trigger.calls))
	}
	if _, err := repo.GetScheduleForUserChannel(ctx, "b-rollback", "u-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetScheduleForUserChannel after failed create err = %v, want ErrNotFound", err)
	}
}

func TestCreate_RejectsEnabledEmptyCategoryOrTagFilters(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-empty-filter")
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, tc := range []struct {
		name  string
		input WriteInput
	}{
		{
			name: "empty categories",
			input: WriteInput{
				BroadcasterID: "b-empty-filter",
				Quality:       repository.QualityHigh,
				HasCategories: true,
			},
		},
		{
			name: "empty tags",
			input: WriteInput{
				BroadcasterID: "b-empty-filter",
				Quality:       repository.QualityHigh,
				HasTags:       true,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(ctx, "u-1", tc.input); !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("Create err = %v, want ErrInvalidFilter", err)
			}
			if _, err := repo.GetScheduleForUserChannel(ctx, "b-empty-filter", "u-1"); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("schedule should not be created after validation failure, err = %v", err)
			}
		})
	}
}

func TestUpdate_RollsBackScalarsAndFiltersWhenFilterLinkFails(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-update-rollback")
	for _, cat := range []repository.Category{
		{ID: "game-1", Name: "Game 1"},
		{ID: "game-2", Name: "Game 2"},
	} {
		if _, err := repo.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	created, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-update-rollback",
		Quality:       repository.QualityLow,
		HasCategories: true,
		CategoryIDs:   []string{"game-1"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		Quality:       repository.QualityHigh,
		HasCategories: true,
		CategoryIDs:   []string{"game-2", "missing-game"},
	}); err == nil {
		t.Fatal("Update with missing category succeeded, want FK error")
	}

	got, err := repo.GetSchedule(ctx, created.Schedule.ID)
	if err != nil {
		t.Fatalf("GetSchedule after failed update: %v", err)
	}
	if got.Quality != repository.QualityLow {
		t.Fatalf("quality after failed update = %q, want original %q", got.Quality, repository.QualityLow)
	}
	cats, err := repo.ListScheduleCategories(ctx, created.Schedule.ID)
	if err != nil {
		t.Fatalf("ListScheduleCategories after failed update: %v", err)
	}
	if len(cats) != 1 || cats[0].ID != "game-1" {
		t.Fatalf("categories after failed update = %+v, want only game-1", cats)
	}
}

func TestUpdate_RejectsEnabledEmptyCategoryOrTagFiltersWithoutClearingExistingLinks(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-update-empty-filter")
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-1", Name: "Game 1"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	tag, err := repo.UpsertTag(ctx, "tag-one")
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	created, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-update-empty-filter",
		Quality:       repository.QualityHigh,
		HasCategories: true,
		CategoryIDs:   []string{"game-1"},
		HasTags:       true,
		TagIDs:        []int64{tag.ID},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityLow,
		HasCategories: true,
		HasTags:       true,
		TagIDs:        []int64{tag.ID},
	}); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("Update empty categories err = %v, want ErrInvalidFilter", err)
	}
	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityLow,
		HasCategories: true,
		CategoryIDs:   []string{"game-1"},
		HasTags:       true,
	}); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("Update empty tags err = %v, want ErrInvalidFilter", err)
	}

	cats, err := repo.ListScheduleCategories(ctx, created.Schedule.ID)
	if err != nil {
		t.Fatalf("ListScheduleCategories: %v", err)
	}
	tags, err := repo.ListScheduleTags(ctx, created.Schedule.ID)
	if err != nil {
		t.Fatalf("ListScheduleTags: %v", err)
	}
	if len(cats) != 1 || cats[0].ID != "game-1" || len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("filters after failed updates = categories %+v tags %+v, want originals", cats, tags)
	}
	got, err := repo.GetSchedule(ctx, created.Schedule.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.Quality != repository.QualityHigh {
		t.Fatalf("quality after failed updates = %q, want %q", got.Quality, repository.QualityHigh)
	}
}

func TestUpdateAndToggle_TriggerOnlyOnActivationOrMatchCriteriaChange(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-1")
	trigger := &fakeLiveTrigger{}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	created, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-1",
		Quality:       repository.QualityLow,
		IsDisabled:    true,
	})
	if err != nil {
		t.Fatalf("Create disabled: %v", err)
	}
	if len(trigger.calls) != 0 {
		t.Fatalf("disabled create trigger calls = %d, want 0", len(trigger.calls))
	}

	disabledMinViewers := int64(5)
	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityMedium,
		HasMinViewers: true,
		MinViewers:    &disabledMinViewers,
		IsDisabled:    true,
	}); err != nil {
		t.Fatalf("Update disabled criteria: %v", err)
	}
	if len(trigger.calls) != 0 {
		t.Fatalf("disabled criteria update trigger calls = %d, want 0", len(trigger.calls))
	}

	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityMedium,
		IsDisabled:    false,
	}); err != nil {
		t.Fatalf("Update enabled: %v", err)
	}
	if len(trigger.calls) != 1 {
		t.Fatalf("enabled update trigger calls = %d, want 1", len(trigger.calls))
	}

	if _, err := svc.Toggle(ctx, "u-1", false, created.Schedule.ID); err != nil {
		t.Fatalf("Toggle disabled: %v", err)
	}
	if len(trigger.calls) != 1 {
		t.Fatalf("disable toggle trigger calls = %d, want still 1", len(trigger.calls))
	}

	if _, err := svc.Toggle(ctx, "u-1", false, created.Schedule.ID); err != nil {
		t.Fatalf("Toggle enabled: %v", err)
	}
	if len(trigger.calls) != 2 {
		t.Fatalf("enable toggle trigger calls = %d, want 2", len(trigger.calls))
	}

	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityHigh,
		ForceH264:     boolPtr(true),
		IsDisabled:    false,
	}); err != nil {
		t.Fatalf("Update quality/codec only: %v", err)
	}
	if len(trigger.calls) != 2 {
		t.Fatalf("quality/codec-only update trigger calls = %d, want still 2", len(trigger.calls))
	}

	minViewers := int64(10)
	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityHigh,
		ForceH264:     boolPtr(true),
		HasMinViewers: true,
		MinViewers:    &minViewers,
		IsDisabled:    false,
	}); err != nil {
		t.Fatalf("Update min-viewer criteria: %v", err)
	}
	if len(trigger.calls) != 3 {
		t.Fatalf("match-criteria update trigger calls = %d, want 3", len(trigger.calls))
	}
}

func TestUpdate_CategoryAndTagCriteriaAreComparedAsSets(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-sets")
	for _, cat := range []repository.Category{
		{ID: "game-1", Name: "Game 1"},
		{ID: "game-2", Name: "Game 2"},
		{ID: "game-3", Name: "Game 3"},
	} {
		if _, err := repo.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}
	tagOne, err := repo.UpsertTag(ctx, "tag-one")
	if err != nil {
		t.Fatalf("seed tag-one: %v", err)
	}
	tagTwo, err := repo.UpsertTag(ctx, "tag-two")
	if err != nil {
		t.Fatalf("seed tag-two: %v", err)
	}
	trigger := &fakeLiveTrigger{}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	created, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-sets",
		Quality:       repository.QualityHigh,
		HasCategories: true,
		CategoryIDs:   []string{"game-1", "game-2"},
		HasTags:       true,
		TagIDs:        []int64{tagOne.ID, tagTwo.ID},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(trigger.calls) != 1 {
		t.Fatalf("create trigger calls = %d, want 1", len(trigger.calls))
	}

	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityLow,
		HasCategories: true,
		CategoryIDs:   []string{"game-2", "game-1", "game-2"},
		HasTags:       true,
		TagIDs:        []int64{tagTwo.ID, tagOne.ID, tagTwo.ID},
	}); err != nil {
		t.Fatalf("Update reordered same sets: %v", err)
	}
	if len(trigger.calls) != 1 {
		t.Fatalf("same-set update trigger calls = %d, want still 1", len(trigger.calls))
	}

	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityLow,
		HasCategories: true,
		CategoryIDs:   []string{"game-3"},
		HasTags:       true,
		TagIDs:        []int64{tagTwo.ID, tagOne.ID},
	}); err != nil {
		t.Fatalf("Update changed category set: %v", err)
	}
	if len(trigger.calls) != 2 {
		t.Fatalf("changed-set update trigger calls = %d, want 2", len(trigger.calls))
	}

	if _, err := svc.Update(ctx, "u-1", false, created.Schedule.ID, WriteInput{
		RecordingType: repository.RecordingTypeVideo,
		Quality:       repository.QualityLow,
		HasCategories: true,
		CategoryIDs:   []string{"game-3"},
		HasTags:       true,
		TagIDs:        []int64{tagTwo.ID},
	}); err != nil {
		t.Fatalf("Update changed tag set: %v", err)
	}
	if len(trigger.calls) != 3 {
		t.Fatalf("changed-tag update trigger calls = %d, want 3", len(trigger.calls))
	}
}

func TestCreate_LiveTriggerErrorDoesNotFailScheduleWrite(t *testing.T) {
	ctx := context.Background()
	repo := newScheduleServiceRepo(t, ctx, "u-1", "b-1")
	trigger := &fakeLiveTrigger{err: errors.New("twitch unavailable")}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)), WithImmediateLiveTrigger(trigger))

	view, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-1",
		Quality:       repository.QualityHigh,
	})
	if err != nil {
		t.Fatalf("Create with trigger error: %v", err)
	}
	if view.Schedule.ID == 0 {
		t.Fatal("schedule was not created")
	}
	if len(trigger.calls) != 1 {
		t.Fatalf("trigger calls = %d, want 1", len(trigger.calls))
	}
}

func TestListMineAndGetByID_EnforceVisibilityAndInflateJunctions(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	seedScheduleUser(t, ctx, repo, "u-1")
	seedScheduleUser(t, ctx, repo, "u-2")
	seedScheduleChannel(t, ctx, repo, "b-1")
	seedScheduleChannel(t, ctx, repo, "b-2")
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-1", Name: "Game 1"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	tag, err := repo.UpsertTag(ctx, "tag-one")
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	first, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-1",
		Quality:       repository.QualityHigh,
		HasCategories: true,
		CategoryIDs:   []string{"game-1"},
		HasTags:       true,
		TagIDs:        []int64{tag.ID},
	})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := svc.Create(ctx, "u-2", WriteInput{
		BroadcasterID: "b-2",
		Quality:       repository.QualityLow,
	})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	ownerViews, err := svc.List(ctx, "any-owner-id", true, 0, 0)
	if err != nil {
		t.Fatalf("List owner: %v", err)
	}
	assertScheduleIDs(t, ownerViews, first.Schedule.ID, second.Schedule.ID)

	userViews, err := svc.List(ctx, "u-1", false, 0, 0)
	if err != nil {
		t.Fatalf("List user: %v", err)
	}
	assertScheduleIDs(t, userViews, first.Schedule.ID)
	assertLinkedFilters(t, findScheduleView(t, userViews, first.Schedule.ID), "game-1", tag.ID)

	mineViews, err := svc.Mine(ctx, "u-2", 0, 0)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	assertScheduleIDs(t, mineViews, second.Schedule.ID)

	own, err := svc.GetByID(ctx, "u-1", false, first.Schedule.ID)
	if err != nil {
		t.Fatalf("GetByID own: %v", err)
	}
	assertLinkedFilters(t, *own, "game-1", tag.ID)

	ownerReadOther, err := svc.GetByID(ctx, "any-owner-id", true, second.Schedule.ID)
	if err != nil {
		t.Fatalf("GetByID owner other: %v", err)
	}
	if ownerReadOther.Schedule.ID != second.Schedule.ID {
		t.Fatalf("owner read schedule id = %d, want %d", ownerReadOther.Schedule.ID, second.Schedule.ID)
	}

	if _, err := svc.GetByID(ctx, "u-2", false, first.Schedule.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("GetByID foreign err = %v, want ErrNotOwner", err)
	}
	if _, err := svc.GetByID(ctx, "u-1", false, 999999); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetByID missing err = %v, want repository.ErrNotFound", err)
	}
}

func TestDelete_EnforcesOwnershipAndCascadesJunctions(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	seedScheduleUser(t, ctx, repo, "u-1")
	seedScheduleUser(t, ctx, repo, "u-2")
	seedScheduleChannel(t, ctx, repo, "b-1")
	seedScheduleChannel(t, ctx, repo, "b-2")
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-1", Name: "Game 1"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	tag, err := repo.UpsertTag(ctx, "tag-one")
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	protected, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-1",
		Quality:       repository.QualityHigh,
		HasCategories: true,
		CategoryIDs:   []string{"game-1"},
		HasTags:       true,
		TagIDs:        []int64{tag.ID},
	})
	if err != nil {
		t.Fatalf("Create protected: %v", err)
	}
	own, err := svc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-2",
		Quality:       repository.QualityLow,
	})
	if err != nil {
		t.Fatalf("Create own: %v", err)
	}

	if err := svc.Delete(ctx, "u-2", false, protected.Schedule.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("Delete foreign err = %v, want ErrNotOwner", err)
	}
	if _, err := repo.GetSchedule(ctx, protected.Schedule.ID); err != nil {
		t.Fatalf("foreign delete removed or hid schedule: %v", err)
	}

	if err := svc.Delete(ctx, "u-1", false, own.Schedule.ID); err != nil {
		t.Fatalf("Delete own: %v", err)
	}
	if _, err := repo.GetSchedule(ctx, own.Schedule.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("own schedule after delete err = %v, want repository.ErrNotFound", err)
	}

	if err := svc.Delete(ctx, "any-owner-id", true, protected.Schedule.ID); err != nil {
		t.Fatalf("Delete owner: %v", err)
	}
	if _, err := repo.GetSchedule(ctx, protected.Schedule.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("protected schedule after owner delete err = %v, want repository.ErrNotFound", err)
	}
	cats, err := repo.ListScheduleCategories(ctx, protected.Schedule.ID)
	if err != nil {
		t.Fatalf("ListScheduleCategories after delete: %v", err)
	}
	tags, err := repo.ListScheduleTags(ctx, protected.Schedule.ID)
	if err != nil {
		t.Fatalf("ListScheduleTags after delete: %v", err)
	}
	if len(cats) != 0 || len(tags) != 0 {
		t.Fatalf("deleted schedule junctions remain: categories=%v tags=%v", cats, tags)
	}

	if err := svc.Delete(ctx, "any-owner-id", true, protected.Schedule.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("Delete missing err = %v, want repository.ErrNotFound", err)
	}
}

func TestReadAndDelete_PropagateRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	seedScheduleUser(t, ctx, repo, "u-1")
	seedScheduleChannel(t, ctx, repo, "b-1")
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-1", Name: "Game 1"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	tag, err := repo.UpsertTag(ctx, "tag-one")
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	baseSvc := New(repo, log)
	created, err := baseSvc.Create(ctx, "u-1", WriteInput{
		BroadcasterID: "b-1",
		Quality:       repository.QualityHigh,
		HasCategories: true,
		CategoryIDs:   []string{"game-1"},
		HasTags:       true,
		TagIDs:        []int64{tag.ID},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	scheduleID := created.Schedule.ID

	listErr := errors.New("list schedules failed")
	svc := New(&scheduleRepoFailure{Repository: repo, listErr: listErr}, log)
	if _, err := svc.List(ctx, "owner-id", true, 50, 0); !errors.Is(err, listErr) {
		t.Fatalf("List owner err = %v, want listErr", err)
	}

	listForUserErr := errors.New("list user schedules failed")
	svc = New(&scheduleRepoFailure{Repository: repo, listForUserErr: listForUserErr}, log)
	if _, err := svc.Mine(ctx, "u-1", 50, 0); !errors.Is(err, listForUserErr) {
		t.Fatalf("Mine err = %v, want listForUserErr", err)
	}

	categoriesErr := errors.New("categories failed")
	svc = New(&scheduleRepoFailure{
		Repository:           repo,
		categoriesErr:        categoriesErr,
		categoriesScheduleID: scheduleID,
	}, log)
	if _, err := svc.List(ctx, "owner-id", true, 50, 0); !errors.Is(err, categoriesErr) {
		t.Fatalf("List inflate categories err = %v, want categoriesErr", err)
	}

	tagsErr := errors.New("tags failed")
	svc = New(&scheduleRepoFailure{
		Repository:     repo,
		tagsErr:        tagsErr,
		tagsScheduleID: scheduleID,
	}, log)
	if _, err := svc.GetByID(ctx, "u-1", false, scheduleID); !errors.Is(err, tagsErr) {
		t.Fatalf("GetByID inflate tags err = %v, want tagsErr", err)
	}

	deleteErr := errors.New("delete failed")
	svc = New(&scheduleRepoFailure{
		Repository:       repo,
		deleteErr:        deleteErr,
		deleteScheduleID: scheduleID,
	}, log)
	if err := svc.Delete(ctx, "u-1", false, scheduleID); !errors.Is(err, deleteErr) {
		t.Fatalf("Delete err = %v, want deleteErr", err)
	}
	if _, err := repo.GetSchedule(ctx, scheduleID); err != nil {
		t.Fatalf("schedule should remain after failed delete: %v", err)
	}
}

func newScheduleServiceRepo(t *testing.T, ctx context.Context, userID, broadcasterID string) repository.Repository {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	seedScheduleUser(t, ctx, repo, userID)
	seedScheduleChannel(t, ctx, repo, broadcasterID)
	return repo
}

func seedScheduleUser(t *testing.T, ctx context.Context, repo repository.Repository, userID string) {
	t.Helper()
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID:          userID,
		Login:       userID,
		DisplayName: userID,
		Role:        "owner",
	}); err != nil {
		t.Fatalf("seed user %s: %v", userID, err)
	}
}

func seedScheduleChannel(t *testing.T, ctx context.Context, repo repository.Repository, broadcasterID string) {
	t.Helper()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: broadcasterID,
		BroadcasterName:  broadcasterID,
	}); err != nil {
		t.Fatalf("seed channel %s: %v", broadcasterID, err)
	}
}

func assertScheduleIDs(t *testing.T, views []View, wantIDs ...int64) {
	t.Helper()
	if len(views) != len(wantIDs) {
		t.Fatalf("schedule views = %d, want %d ids %v", len(views), len(wantIDs), wantIDs)
	}
	got := make(map[int64]bool, len(views))
	for _, v := range views {
		if v.Schedule == nil {
			t.Fatalf("schedule view has nil Schedule: %+v", v)
		}
		got[v.Schedule.ID] = true
	}
	for _, id := range wantIDs {
		if !got[id] {
			t.Fatalf("schedule ids = %v, missing %d", got, id)
		}
	}
}

func findScheduleView(t *testing.T, views []View, id int64) View {
	t.Helper()
	for _, v := range views {
		if v.Schedule != nil && v.Schedule.ID == id {
			return v
		}
	}
	t.Fatalf("schedule id %d not found in views", id)
	return View{}
}

func assertLinkedFilters(t *testing.T, view View, categoryID string, tagID int64) {
	t.Helper()
	if len(view.Categories) != 1 || view.Categories[0].ID != categoryID {
		t.Fatalf("categories = %+v, want one category %q", view.Categories, categoryID)
	}
	if len(view.Tags) != 1 || view.Tags[0].ID != tagID {
		t.Fatalf("tags = %+v, want one tag %d", view.Tags, tagID)
	}
}
