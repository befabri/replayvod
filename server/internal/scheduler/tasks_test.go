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
	"github.com/befabri/replayvod/server/internal/service/categoryart"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
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
func registeredIntervals(t *testing.T, cfg *config.Config, esvc *eventsub.Service, artsvc *categoryart.Service) map[string]int64 {
	t.Helper()
	s, repo := newTestScheduler(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := RegisterStandardTasks(s, cfg, repo, esvc, artsvc, log); err != nil {
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
// "everything on" contract: with every interval populated and both optional
// services present, exactly these eight tasks register, each carrying the
// interval derived from its own config field. Distinct minute values catch a
// crossed wire (e.g. reconcile reading EventsubIntervalMinutes), and the three
// retention tasks must land on the fixed daily cadence (86400s) rather than
// scaling with their retention-day count.
func TestRegisterStandardTasks_FullConfigRegistersExactlyExpectedSet(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				TokenCleanupIntervalMinutes:      60,
				SessionCleanupIntervalMinutes:    120,
				FetchLogsRetentionDays:           14,
				WebhookEventPayloadRetentionDays: 7,
				EventLogsRetentionDays:           30,
				EventsubReconcileIntervalMinutes: 15,
				EventsubIntervalMinutes:          10,
				CategoryArtIntervalMinutes:       45,
			},
		},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
	}

	got := registeredIntervals(t, cfg, eventsubService(t), categoryArtService(t))
	want := map[string]int64{
		"app_token_cleanup":           60 * 60,
		"session_cleanup":             120 * 60,
		"fetch_logs_retention":        dailySeconds,
		"webhook_payload_trim":        dailySeconds,
		"event_logs_retention":        dailySeconds,
		taskEventSubReconcileChannels: 15 * 60,
		taskEventSubSnapshot:          10 * 60,
		// 45 (not the production 1440) so the expected interval (2700) is
		// distinct from the daily constant; otherwise a mutant that hands
		// category_art_sync the fixed daily cadence instead of minutes*60
		// would pass unnoticed.
		"category_art_sync": 45 * 60,
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

	got := registeredIntervals(t, cfg, nil, nil)
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Enabled: only this field is positive.
			enabled := config.SchedulerConfig{}
			tc.mutate(&enabled)
			cfg := &config.Config{App: config.AppConfig{Scheduler: enabled}}
			got := registeredIntervals(t, cfg, nil, nil)
			if iv, ok := got[tc.taskName]; !ok {
				t.Fatalf("%s not registered when its interval is positive", tc.taskName)
			} else if iv != tc.wantInterval {
				t.Fatalf("%s interval = %d, want %d", tc.taskName, iv, tc.wantInterval)
			}

			// Disabled: a zero SchedulerConfig must omit it entirely.
			zero := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{}}}
			gotZero := registeredIntervals(t, zero, nil, nil)
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
			got := registeredIntervals(t, cfg, nil, artsvc)
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
			got := registeredIntervals(t, cfg, eventsubService(t), nil)
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
				TokenCleanupIntervalMinutes:      60,
				SessionCleanupIntervalMinutes:    60,
				FetchLogsRetentionDays:           14,
				WebhookEventPayloadRetentionDays: 7,
				EventLogsRetentionDays:           30,
			},
		},
	}
	if err := RegisterStandardTasks(s, cfg, repo, nil, nil, log); err != nil {
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
	if err := RegisterStandardTasks(s, cfg, repo, eventsubService(t), nil, log); err != nil {
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
	if err := RegisterStandardTasks(s, cfg, repo, esvc, nil, log); err != nil {
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
	if err := RegisterStandardTasks(s, cfg, repo, nil, artsvc, log); err != nil {
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
