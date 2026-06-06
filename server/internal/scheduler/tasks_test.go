package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/categoryart"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const dailySeconds int64 = 24 * 60 * 60

// registeredIntervals runs RegisterStandardTasks against a fresh scheduler and
// returns name -> IntervalSeconds for every task it wired.
//
// It reads s.tasks directly instead of Start()-ing the scheduler. Starting
// would tick immediately and fire real task bodies (the EventSub reconcile
// would hit the Twitch client); the in-memory registration is the contract
// RegisterStandardTasks owns. Reading the map without the mutex is safe here
// because nothing is started, so no ticker goroutine touches it.
func registeredIntervals(t *testing.T, cfg *config.Config, esvc *eventsub.Service, artsvc *categoryart.Service, retentionsvc *retention.Service) map[string]int64 {
	t.Helper()
	s, repo := newTestScheduler(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := RegisterStandardTasks(s, cfg, repo, esvc, artsvc, retentionsvc, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}
	out := make(map[string]int64, len(s.tasks))
	for name, task := range s.tasks {
		out[name] = task.IntervalSeconds
	}
	return out
}

func assertExactTasks(t *testing.T, got, want map[string]int64) {
	t.Helper()
	// fmt prints map keys sorted, so the diff is stable and readable.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("registered task set mismatch\n got: %v\nwant: %v", got, want)
	}
}

func eventsubService(t *testing.T) *eventsub.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, repo := newTestScheduler(t)
	tc := twitch.NewClient("client-id", "client-secret", log)
	return eventsub.New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "0123456789abcdef", log)
}

func categoryArtService(t *testing.T) *categoryart.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, repo := newTestScheduler(t)
	// tc may be nil; we never run the task body, only assert registration.
	return categoryart.New(repo, nil, log)
}

func retentionService(t *testing.T) *retention.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, repo := newTestScheduler(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	return retention.New(repo, store, log)
}

// TestRetentionCutoff pins the day-subtraction every daily retention task
// shares: the cutoff is exactly `days` days before the reference instant and
// lies in the past. The task closures call time.Now() and delegate the math
// here, so this is the one place a flipped sign (a future cutoff that deletes
// everything) or a wrong unit (months instead of days) is caught. The
// month- and year-crossing cases guard against naive day-of-month arithmetic.
func TestRetentionCutoff(t *testing.T) {
	now := time.Date(2026, time.May, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		days int
		want time.Time
	}{
		{"one day", 1, time.Date(2026, time.May, 30, 12, 0, 0, 0, time.UTC)},
		{"crosses month boundary", 31, time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)},
		{"crosses year boundary", 365, time.Date(2025, time.May, 31, 12, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := retentionCutoff(now, tc.days)
			if !got.Equal(tc.want) {
				t.Errorf("retentionCutoff(%s, %d) = %s, want %s", now, tc.days, got, tc.want)
			}
			if !got.Before(now) {
				t.Errorf("retentionCutoff(%s, %d) = %s, want an instant before now", now, tc.days, got)
			}
		})
	}
}

type taskBodyRepo struct {
	repository.Repository
	calls []string
	err   error
}

func (r *taskBodyRepo) record(name string) error {
	r.calls = append(r.calls, name)
	return r.err
}

func (r *taskBodyRepo) DeleteExpiredAppTokens(context.Context) error {
	return r.record("DeleteExpiredAppTokens")
}

func (r *taskBodyRepo) DeleteExpiredSessions(context.Context) error {
	return r.record("DeleteExpiredSessions")
}

func (r *taskBodyRepo) DeleteOldFetchLogs(context.Context, time.Time) error {
	return r.record("DeleteOldFetchLogs")
}

func (r *taskBodyRepo) ClearWebhookEventPayload(context.Context, time.Time) error {
	return r.record("ClearWebhookEventPayload")
}

func (r *taskBodyRepo) DeleteOldEventLogs(context.Context, time.Time) error {
	return r.record("DeleteOldEventLogs")
}

func (r *taskBodyRepo) DeleteOldRecordingWebhookDeliveries(context.Context, time.Time) error {
	return r.record("DeleteOldRecordingWebhookDeliveries")
}

func (r *taskBodyRepo) ListChannels(ctx context.Context) ([]repository.Channel, error) {
	if r.err != nil {
		r.calls = append(r.calls, "ListChannels")
		return nil, r.err
	}
	return r.Repository.ListChannels(ctx)
}

type schedulerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f schedulerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func schedulerTextResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type schedulerFakeGames struct {
	calls [][]string
	byID  map[string]string
}

func (f *schedulerFakeGames) GetGames(_ context.Context, params *twitch.GetGamesParams) ([]twitch.Game, error) {
	ids := append([]string(nil), params.ID...)
	f.calls = append(f.calls, ids)
	out := make([]twitch.Game, 0, len(ids))
	for _, id := range ids {
		if art, ok := f.byID[id]; ok {
			out = append(out, twitch.Game{ID: id, Name: "Game " + id, BoxArtURL: art})
		}
	}
	return out, nil
}

// TestRegisterStandardTasks_FullConfigRegistersExactlyExpectedSet pins the
// "everything on" contract: with every interval populated and all optional
// services present, exactly these eleven tasks register, each carrying the
// interval derived from its own config field. Distinct minute values catch a
// crossed wire (e.g. reconcile reading EventsubIntervalMinutes), and the four
// log-retention tasks must land on the fixed daily cadence (86400s) rather than
// scaling with their retention-day count.
func TestRegisterStandardTasks_FullConfigRegistersExactlyExpectedSet(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				TokenCleanupIntervalMinutes:           60,
				SessionCleanupIntervalMinutes:         120,
				FetchLogsRetentionDays:                14,
				WebhookEventPayloadRetentionDays:      7,
				EventLogsRetentionDays:                30,
				RecordingWebhookDeliveryRetentionDays: 11,
				EventsubReconcileIntervalMinutes:      15,
				EventsubIntervalMinutes:               10,
				CategoryArtIntervalMinutes:            45,
				RecordingsRetentionIntervalMinutes:    30,
			},
		},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
	}

	got := registeredIntervals(t, cfg, eventsubService(t), categoryArtService(t), retentionService(t))
	want := map[string]int64{
		"app_token_cleanup":                      60 * 60,
		"session_cleanup":                        120 * 60,
		"fetch_logs_retention":                   dailySeconds,
		"webhook_payload_trim":                   dailySeconds,
		"event_logs_retention":                   dailySeconds,
		"recording_webhook_deliveries_retention": dailySeconds,
		taskEventSubReconcileChannels:            15 * 60,
		taskEventSubSnapshot:                     10 * 60,
		// 45 (not the production 1440) so the expected interval (2700) is
		// distinct from the daily constant; otherwise a mutant that hands
		// category_art_sync the fixed daily cadence instead of minutes*60
		// would pass unnoticed.
		"category_art_sync":              45 * 60,
		retention.ManualDeletionTaskName: retention.ManualDeletionIntervalSeconds,
		// 30 min → 1800s, a poll cadence derived from the field (not a
		// retention-day count), distinct from every other value above.
		"recordings_retention": 30 * 60,
	}
	assertExactTasks(t, got, want)
}

// TestRegisterStandardTasks_ZeroConfigRegistersOnlyNeutralizedEventSubPair is
// the mirror image: an empty SchedulerConfig with no optional services (the
// off/poll-mode shape, where main.go passes esvc == nil). Every config-gated
// task is skipped because its interval is 0, and the only rows that remain are
// the two EventSub tasks, which always register but neutralized to interval 0
// so ListDueTasks never returns them. This is the assertion that proves the
// config-gated tasks are genuinely absent (not merely disabled) when off.
func TestRegisterStandardTasks_ZeroConfigRegistersOnlyNeutralizedEventSubPair(t *testing.T) {
	cfg := &config.Config{
		App:        config.AppConfig{Scheduler: config.SchedulerConfig{}},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeOff},
	}

	got := registeredIntervals(t, cfg, nil, nil, nil)
	want := map[string]int64{
		taskEventSubReconcileChannels: 0,
		taskEventSubSnapshot:          0,
	}
	assertExactTasks(t, got, want)
}

// TestRegisterStandardTasks_ConfigGatedTasksByInterval pins each config-only
// task one field at a time: a positive interval registers exactly that task
// (plus the always-on EventSub pair) with the right cadence, and a zero
// interval leaves it unregistered. Toggling one field in isolation guarantees
// the gate is wired to that task's own field and nothing else.
func TestRegisterStandardTasks_ConfigGatedTasksByInterval(t *testing.T) {
	cases := []struct {
		name         string
		mutate       func(*config.SchedulerConfig)
		taskName     string
		wantInterval int64
	}{
		{
			name:         "app_token_cleanup",
			mutate:       func(sc *config.SchedulerConfig) { sc.TokenCleanupIntervalMinutes = 45 },
			taskName:     "app_token_cleanup",
			wantInterval: 45 * 60,
		},
		{
			name:         "session_cleanup",
			mutate:       func(sc *config.SchedulerConfig) { sc.SessionCleanupIntervalMinutes = 90 },
			taskName:     "session_cleanup",
			wantInterval: 90 * 60,
		},
		{
			name:         "fetch_logs_retention",
			mutate:       func(sc *config.SchedulerConfig) { sc.FetchLogsRetentionDays = 21 },
			taskName:     "fetch_logs_retention",
			wantInterval: dailySeconds,
		},
		{
			name:         "webhook_payload_trim",
			mutate:       func(sc *config.SchedulerConfig) { sc.WebhookEventPayloadRetentionDays = 3 },
			taskName:     "webhook_payload_trim",
			wantInterval: dailySeconds,
		},
		{
			name:         "event_logs_retention",
			mutate:       func(sc *config.SchedulerConfig) { sc.EventLogsRetentionDays = 5 },
			taskName:     "event_logs_retention",
			wantInterval: dailySeconds,
		},
		{
			name:         "recording_webhook_deliveries_retention",
			mutate:       func(sc *config.SchedulerConfig) { sc.RecordingWebhookDeliveryRetentionDays = 11 },
			taskName:     "recording_webhook_deliveries_retention",
			wantInterval: dailySeconds,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Enabled: only this field is positive.
			enabled := config.SchedulerConfig{}
			tc.mutate(&enabled)
			cfg := &config.Config{App: config.AppConfig{Scheduler: enabled}}
			got := registeredIntervals(t, cfg, nil, nil, nil)
			if iv, ok := got[tc.taskName]; !ok {
				t.Fatalf("%s not registered when its interval is positive", tc.taskName)
			} else if iv != tc.wantInterval {
				t.Fatalf("%s interval = %d, want %d", tc.taskName, iv, tc.wantInterval)
			}

			// Disabled: a zero SchedulerConfig must omit it entirely.
			zero := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{}}}
			gotZero := registeredIntervals(t, zero, nil, nil, nil)
			if _, ok := gotZero[tc.taskName]; ok {
				t.Fatalf("%s registered with a zero interval; want unregistered", tc.taskName)
			}
		})
	}
}

// TestRegisterStandardTasks_CategoryArtGating pins the dual gate on the box-art
// backfill: it needs BOTH a non-nil artsvc and a positive interval. A nil
// service (degraded mode, eager Hydrator only) suppresses it even with a live
// interval, and a present service with a zero interval stays unregistered.
func TestRegisterStandardTasks_CategoryArtGating(t *testing.T) {
	const taskName = "category_art_sync"
	cases := []struct {
		name        string
		interval    int
		withService bool
		wantPresent bool
	}{
		// A minute value whose ×60 (2700) is not the daily 86400, so the
		// interval assertion also kills a "use the daily constant" mutant.
		{name: "service and interval", interval: 45, withService: true, wantPresent: true},
		{name: "service but zero interval", interval: 0, withService: true, wantPresent: false},
		{name: "interval but nil service", interval: 45, withService: false, wantPresent: false},
		{name: "neither", interval: 0, withService: false, wantPresent: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				App: config.AppConfig{Scheduler: config.SchedulerConfig{CategoryArtIntervalMinutes: tc.interval}},
			}
			var artsvc *categoryart.Service
			if tc.withService {
				artsvc = categoryArtService(t)
			}
			got := registeredIntervals(t, cfg, nil, artsvc, nil)
			iv, present := got[taskName]
			if present != tc.wantPresent {
				t.Fatalf("%s present = %v, want %v", taskName, present, tc.wantPresent)
			}
			if tc.wantPresent && iv != int64(tc.interval)*60 {
				t.Fatalf("%s interval = %d, want %d", taskName, iv, int64(tc.interval)*60)
			}
		})
	}
}

// TestRegisterStandardTasks_EventSubConditionsAreIndependent pins that the two
// EventSub tasks are gated by separate config fields, not a shared toggle. With
// the service present but only the reconcile interval positive, reconcile must
// register active while snapshot is neutralized to 0, and vice versa. This also
// covers the esvc-present-but-interval-zero corner the active-branch test never
// reaches (it sets both intervals positive).
func TestRegisterStandardTasks_EventSubConditionsAreIndependent(t *testing.T) {
	cases := []struct {
		name          string
		reconcileMin  int
		snapshotMin   int
		wantReconcile int64
		wantSnapshot  int64
	}{
		{name: "reconcile only", reconcileMin: 15, snapshotMin: 0, wantReconcile: 15 * 60, wantSnapshot: 0},
		{name: "snapshot only", reconcileMin: 0, snapshotMin: 10, wantReconcile: 0, wantSnapshot: 10 * 60},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				App: config.AppConfig{
					Scheduler: config.SchedulerConfig{
						EventsubReconcileIntervalMinutes: tc.reconcileMin,
						EventsubIntervalMinutes:          tc.snapshotMin,
					},
				},
				ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
			}
			got := registeredIntervals(t, cfg, eventsubService(t), nil, nil)
			if got[taskEventSubReconcileChannels] != tc.wantReconcile {
				t.Fatalf("%s interval = %d, want %d", taskEventSubReconcileChannels, got[taskEventSubReconcileChannels], tc.wantReconcile)
			}
			if got[taskEventSubSnapshot] != tc.wantSnapshot {
				t.Fatalf("%s interval = %d, want %d", taskEventSubSnapshot, got[taskEventSubSnapshot], tc.wantSnapshot)
			}
		})
	}
}

// TestRegisterStandardTasks_ConfigTaskBodiesCallExpectedRepoMethods invokes the
// registered Run closures for the repository-backed tasks. Registration-only
// tests would miss a crossed wire where the task name/interval is right but the
// closure calls the wrong repository method.
func TestRegisterStandardTasks_ConfigTaskBodiesCallExpectedRepoMethods(t *testing.T) {
	s, baseRepo := newTestScheduler(t)
	sentinel := errors.New("sentinel task body error")
	repo := &taskBodyRepo{Repository: baseRepo, err: sentinel}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				TokenCleanupIntervalMinutes:           60,
				SessionCleanupIntervalMinutes:         60,
				FetchLogsRetentionDays:                14,
				WebhookEventPayloadRetentionDays:      7,
				EventLogsRetentionDays:                30,
				RecordingWebhookDeliveryRetentionDays: 11,
			},
		},
	}
	if err := RegisterStandardTasks(s, cfg, repo, nil, nil, nil, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}

	cases := []struct {
		taskName string
		method   string
	}{
		{"app_token_cleanup", "DeleteExpiredAppTokens"},
		{"session_cleanup", "DeleteExpiredSessions"},
		{"fetch_logs_retention", "DeleteOldFetchLogs"},
		{"webhook_payload_trim", "ClearWebhookEventPayload"},
		{"event_logs_retention", "DeleteOldEventLogs"},
		{"recording_webhook_deliveries_retention", "DeleteOldRecordingWebhookDeliveries"},
	}
	for _, tc := range cases {
		t.Run(tc.taskName, func(t *testing.T) {
			task, ok := s.tasks[tc.taskName]
			if !ok {
				t.Fatalf("%s was not registered", tc.taskName)
			}
			repo.calls = nil
			err := task.Run(context.Background())
			if !errors.Is(err, sentinel) {
				t.Fatalf("%s Run() error = %v, want sentinel from %s", tc.taskName, err, tc.method)
			}
			if !reflect.DeepEqual(repo.calls, []string{tc.method}) {
				t.Fatalf("%s calls = %v, want [%s]", tc.taskName, repo.calls, tc.method)
			}
		})
	}
}

func TestRegisterStandardTasks_EventSubReconcileTaskListsChannels(t *testing.T) {
	s, baseRepo := newTestScheduler(t)
	sentinel := errors.New("list channels failed")
	repo := &taskBodyRepo{Repository: baseRepo, err: sentinel}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{EventsubReconcileIntervalMinutes: 15},
		},
	}
	if err := RegisterStandardTasks(s, cfg, repo, eventsubService(t), nil, nil, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}

	task := s.tasks[taskEventSubReconcileChannels]
	err := task.Run(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("reconcile Run() error = %v, want ListChannels sentinel", err)
	}
	if !reflect.DeepEqual(repo.calls, []string{"ListChannels"}) {
		t.Fatalf("calls = %v, want [ListChannels]", repo.calls)
	}
}

func TestRegisterStandardTasks_EventSubSnapshotTaskRunsSnapshot(t *testing.T) {
	s, repo := newTestScheduler(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tc := twitch.NewClient("client-id", "client-secret", log)
	tc.SetHTTPClient(&http.Client{
		Transport: schedulerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return schedulerTextResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
				return schedulerTextResponse(http.StatusOK, `{"data":[],"pagination":{},"total":0,"total_cost":0,"max_total_cost":10000}`), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	esvc := eventsub.New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "0123456789abcdef", log)
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{EventsubIntervalMinutes: 10},
		},
	}
	if err := RegisterStandardTasks(s, cfg, repo, esvc, nil, nil, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}

	if err := s.tasks[taskEventSubSnapshot].Run(context.Background()); err != nil {
		t.Fatalf("snapshot Run(): %v", err)
	}
	snap, err := repo.GetLatestEventSubSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetLatestEventSubSnapshot: %v", err)
	}
	if snap.MaxTotalCost != 10000 {
		t.Fatalf("snapshot = %+v, want max_total_cost 10000 from fake Twitch response", snap)
	}
}

func TestRegisterStandardTasks_CategoryArtTaskRunsSyncMissing(t *testing.T) {
	s, repo := newTestScheduler(t)
	ctx := context.Background()
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-42", Name: "Game 42"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	fakeGames := &schedulerFakeGames{
		byID: map[string]string{"game-42": "https://static-cdn.jtvnw.net/ttv-boxart/game-42-{width}x{height}.jpg"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	artsvc := categoryart.New(repo, fakeGames, log)
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{CategoryArtIntervalMinutes: 45},
		},
	}
	if err := RegisterStandardTasks(s, cfg, repo, nil, artsvc, nil, log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}

	if err := s.tasks["category_art_sync"].Run(ctx); err != nil {
		t.Fatalf("category_art_sync Run(): %v", err)
	}
	if len(fakeGames.calls) != 1 || !reflect.DeepEqual(fakeGames.calls[0], []string{"game-42"}) {
		t.Fatalf("GetGames calls = %v, want [[game-42]]", fakeGames.calls)
	}
	cat, err := repo.GetCategory(ctx, "game-42")
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if cat.BoxArtURL == nil || *cat.BoxArtURL != "https://static-cdn.jtvnw.net/ttv-boxart/game-42-{width}x{height}.jpg" {
		t.Fatalf("BoxArtURL = %v, want fake art URL", cat.BoxArtURL)
	}
}

// TestRegisterStandardTasks_RecordingsRetentionGating pins the dual gate on
// the auto-delete sweep: it needs BOTH a non-nil retention service and a
// positive interval. A nil service (no storage backend) suppresses it even
// with a live interval, and a present service with a zero interval stays
// unregistered — the operator's "0 disables" escape hatch.
func TestRegisterStandardTasks_RecordingsRetentionGating(t *testing.T) {
	const taskName = "recordings_retention"
	cases := []struct {
		name        string
		interval    int
		withService bool
		wantPresent bool
	}{
		// 30 min → 1800s, distinct from the daily 86400 so the interval
		// assertion also kills a "use the daily constant" mutant.
		{name: "service and interval", interval: 30, withService: true, wantPresent: true},
		{name: "service but zero interval", interval: 0, withService: true, wantPresent: false},
		{name: "interval but nil service", interval: 30, withService: false, wantPresent: false},
		{name: "neither", interval: 0, withService: false, wantPresent: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				App: config.AppConfig{Scheduler: config.SchedulerConfig{RecordingsRetentionIntervalMinutes: tc.interval}},
			}
			var retsvc *retention.Service
			if tc.withService {
				retsvc = retentionService(t)
			}
			got := registeredIntervals(t, cfg, nil, nil, retsvc)
			iv, present := got[taskName]
			if present != tc.wantPresent {
				t.Fatalf("%s present = %v, want %v", taskName, present, tc.wantPresent)
			}
			if tc.wantPresent && iv != int64(tc.interval)*60 {
				t.Fatalf("%s interval = %d, want %d", taskName, iv, int64(tc.interval)*60)
			}
		})
	}
}

// TestRegisterStandardTasks_RecordingsRetentionTaskDeletesExpired runs the
// registered closure end to end: it sweeps with the real clock, so a freshly
// seeded recording is backdated past its 1h window, and the task must
// tombstone the row and remove the stored object. Registration-only tests
// would miss a closure wired to the wrong clock or method.
func TestRegisterStandardTasks_RecordingsRetentionTaskDeletesExpired(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// FK parents + a schedule that auto-deletes 1h after completion.
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "u-1", Login: "u-1", DisplayName: "u-1", Role: "viewer"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b-1", BroadcasterName: "b-1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	hour := int64(1)
	if _, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH",
		IsDeleteRediff: true, TimeBeforeDelete: &hour,
	}); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	// A finished recording with one stored part.
	vid, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-1", Filename: "rec1", DisplayName: "b-1", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: &hour,
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID: vid.ID, PartIndex: 1, Filename: "rec1-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
	}); err != nil {
		t.Fatalf("create part: %v", err)
	}
	if err := repo.MarkVideoDone(ctx, vid.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	// MarkVideoDone always stamps downloaded_at = now(); backdate it past
	// the 1h window so the closure's real-clock sweep finds it expired.
	if _, err := db.ExecContext(ctx, "UPDATE videos SET downloaded_at = datetime('now','-2 hours') WHERE id = ?", vid.ID); err != nil {
		t.Fatalf("backdate completion: %v", err)
	}
	if err := store.Save(ctx, "videos/rec1-part01.mp4", strings.NewReader("data")); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	s := NewService(repo, log, 20*time.Millisecond, nil)
	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{RecordingsRetentionIntervalMinutes: 30}}}
	if err := RegisterStandardTasks(s, cfg, repo, nil, nil, retention.New(repo, store, log), log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}

	if err := s.tasks["recordings_retention"].Run(ctx); err != nil {
		t.Fatalf("recordings_retention Run(): %v", err)
	}

	got, err := repo.GetVideo(ctx, vid.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("expired recording not tombstoned; deleted_at is nil")
	}
	if ok, _ := store.Exists(ctx, "videos/rec1-part01.mp4"); ok {
		t.Fatalf("video object still present after retention sweep")
	}
}

// deleteFailStore wraps a real storage backend but fails every Delete, so a
// retention sweep that finds an expired recording errors mid-purge. Reads
// (Exists/Stat/Open) still work via the embedded store.
type deleteFailStore struct {
	storage.Storage
	err error
}

func (s deleteFailStore) Delete(context.Context, string) error { return s.err }

// TestRegisterStandardTasks_RecordingsRetentionTaskPropagatesError pins that a
// sweep failure surfaces out of the registered closure rather than being
// swallowed, so the scheduler marks the task run failed and the operator sees
// it. Without this, a storage outage during retention would look like a
// successful no-op sweep.
func TestRegisterStandardTasks_RecordingsRetentionTaskPropagatesError(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// FK parents + an auto-delete schedule + a finished, expired recording, so
	// the sweep selects it and tries (and fails) to purge its object.
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "u-1", Login: "u-1", DisplayName: "u-1", Role: "viewer"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b-1", BroadcasterName: "b-1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	hour := int64(1)
	if _, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH",
		IsDeleteRediff: true, TimeBeforeDelete: &hour,
	}); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	vid, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-1", Filename: "rec1", DisplayName: "b-1", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: &hour,
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID: vid.ID, PartIndex: 1, Filename: "rec1-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
	}); err != nil {
		t.Fatalf("create part: %v", err)
	}
	if err := repo.MarkVideoDone(ctx, vid.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE videos SET downloaded_at = datetime('now','-2 hours') WHERE id = ?", vid.ID); err != nil {
		t.Fatalf("backdate completion: %v", err)
	}

	boom := errors.New("storage offline")
	store := deleteFailStore{Storage: local, err: boom}
	s := NewService(repo, log, 20*time.Millisecond, nil)
	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{RecordingsRetentionIntervalMinutes: 30}}}
	if err := RegisterStandardTasks(s, cfg, repo, nil, nil, retention.New(repo, store, log), log); err != nil {
		t.Fatalf("RegisterStandardTasks: %v", err)
	}

	runErr := s.tasks["recordings_retention"].Run(ctx)
	if runErr == nil {
		t.Fatal("recordings_retention Run() returned nil; a sweep failure must propagate to the scheduler")
	}
	if !errors.Is(runErr, boom) {
		t.Fatalf("propagated error %v does not wrap the storage failure %v", runErr, boom)
	}
	// The failed purge must leave the DB untouched so the next sweep retries.
	got, err := repo.GetVideo(ctx, vid.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatal("recording tombstoned despite a failed object purge; retry would never reclaim the bytes")
	}
}
