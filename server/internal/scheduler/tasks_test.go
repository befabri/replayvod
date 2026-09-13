package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/igdb"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/archiveposter"
	"github.com/befabri/replayvod/server/internal/service/categoryart"
	"github.com/befabri/replayvod/server/internal/service/categorymeta"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/replayvod/server/internal/service/storagescan"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const dailySeconds int64 = 24 * 60 * 60

func builtIntervals(t *testing.T, cfg *config.Config, deps StandardTaskDeps) map[string]int64 {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	tasks := BuildStandardTasks(cfg, nil, deps, log)
	newTestRegistry(t, tasks...)
	out := make(map[string]int64, len(tasks))
	for _, task := range tasks {
		out[task.Name] = task.IntervalSeconds
	}
	return out
}

func builtTasks(t *testing.T, cfg *config.Config, repo repository.Repository, deps StandardTaskDeps, log *slog.Logger) map[string]Task {
	t.Helper()
	return newTestRegistry(t, BuildStandardTasks(cfg, repo, deps, log)...).tasks
}

func assertExactTasks(t *testing.T, got, want map[string]int64) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("registered task set mismatch\n got: %v\nwant: %v", got, want)
	}
}

func eventsubService(t *testing.T) *eventsub.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := newTestRepo(t)
	tc := twitch.NewClient("client-id", "client-secret", log)
	return eventsub.New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "0123456789abcdef", log)
}

func categoryArtService(t *testing.T) *categoryart.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := newTestRepo(t)
	// tc may be nil; we never run the task body, only assert registration.
	return categoryart.New(repo, nil, log)
}

func categoryMetadataService(t *testing.T) *categorymeta.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := newTestRepo(t)
	return categorymeta.New(repo, nil, log)
}

func retentionService(t *testing.T) *retention.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := newTestRepo(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	return retention.New(repo, mediatest.New(t, repo, store, readyStorage{}, nil), log)
}

func archivePosterService(t *testing.T) *archiveposter.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := newTestRepo(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	return archiveposter.New(archiveposter.NewStore(repo, mediatest.New(t, repo, store, nil, nil), &http.Client{Timeout: time.Second}, log), repo, nil, log)
}

func storageScanService(t *testing.T) *storagescan.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := newTestRepo(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	return storagescan.New(repo, mediatest.New(t, repo, store, nil, nil), log)
}

// TestRetentionCutoff guards month and year boundaries against a future cutoff that purges current rows.
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
	calls       []string
	err         error
	fetchCutoff time.Time
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

func (r *taskBodyRepo) DeleteOldFetchLogs(_ context.Context, cutoff time.Time) error {
	r.fetchCutoff = cutoff
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
	byID  map[string]twitch.Game
}

func (f *schedulerFakeGames) GetGames(_ context.Context, params *twitch.GetGamesParams) ([]twitch.Game, error) {
	ids := append([]string(nil), params.ID...)
	f.calls = append(f.calls, ids)
	out := make([]twitch.Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := f.byID[id]; ok {
			if game.ID == "" {
				game.ID = id
			}
			if game.Name == "" {
				game.Name = "Game " + id
			}
			out = append(out, game)
		}
	}
	return out, nil
}

type schedulerFakeIGDB struct {
	calls [][]int64
	byID  map[int64]igdb.Game
}

func (f *schedulerFakeIGDB) GetGames(_ context.Context, ids []int64) ([]igdb.Game, error) {
	call := append([]int64(nil), ids...)
	f.calls = append(f.calls, call)
	out := make([]igdb.Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := f.byID[id]; ok {
			if game.ID == 0 {
				game.ID = id
			}
			out = append(out, game)
		}
	}
	return out, nil
}

// TestBuildStandardTasks_FullConfigRegistersExactlyExpectedSet uses distinct intervals to expose crossed configuration fields.
func TestBuildStandardTasks_FullConfigRegistersExactlyExpectedSet(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				Enabled:                               true,
				TokenCleanupIntervalMinutes:           60,
				SessionCleanupIntervalMinutes:         120,
				FetchLogsRetentionDays:                14,
				WebhookEventPayloadRetentionDays:      7,
				EventLogsRetentionDays:                30,
				RecordingWebhookDeliveryRetentionDays: 11,
				EventsubReconcileIntervalMinutes:      15,
				EventsubIntervalMinutes:               10,
				CategoryArtIntervalMinutes:            45,
				CategoryMetadataIntervalMinutes:       75,
				RecordingsRetentionIntervalMinutes:    30,
				StorageScanIntervalMinutes:            20,
				ArchivePosterIntervalMinutes:          7,
			},
		},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
	}

	deps := StandardTaskDeps{
		EventSub:               eventsubService(t),
		CategoryArt:            categoryArtService(t),
		CategoryMetadata:       categoryMetadataService(t),
		Retention:              retentionService(t),
		StorageScan:            storageScanService(t),
		ArchivePosters:         archivePosterService(t),
		PlaybackCacheReconcile: func(context.Context) error { t.Error("builder ran playback maintenance"); return nil },
	}
	got := builtIntervals(t, cfg, deps)
	want := map[string]int64{
		"app_token_cleanup":                      60 * 60,
		"session_cleanup":                        120 * 60,
		"fetch_logs_retention":                   dailySeconds,
		"webhook_payload_trim":                   dailySeconds,
		"event_logs_retention":                   dailySeconds,
		"recording_webhook_deliveries_retention": dailySeconds,
		taskEventSubReconcileChannels:            15 * 60,
		taskEventSubSnapshot:                     10 * 60,
		// A nondaily interval exposes accidental use of the daily maintenance cadence.
		"category_art_sync":              45 * 60,
		"category_metadata_sync":         75 * 60,
		retention.ManualDeletionTaskName: retention.ManualDeletionIntervalSeconds,
		"recordings_retention":           30 * 60,
		TaskStorageScan:                  20 * 60,
		taskArchivePosters:               7 * 60,
		taskPlaybackCacheReconcile:       5 * 60,
	}
	assertExactTasks(t, got, want)

	cfg.App.Scheduler.Enabled = false
	assertExactTasks(t, builtIntervals(t, cfg, deps), map[string]int64{})
}

func TestBuildStandardTasks_ZeroConfigRegistersNoTasks(t *testing.T) {
	cfg := &config.Config{
		App:        config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true}},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeOff},
	}

	got := builtIntervals(t, cfg, StandardTaskDeps{})
	want := map[string]int64{}
	assertExactTasks(t, got, want)
}

func TestBuildStandardTasks_ConfigGatedTasksByInterval(t *testing.T) {
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
			enabled := config.SchedulerConfig{Enabled: true}
			tc.mutate(&enabled)
			cfg := &config.Config{App: config.AppConfig{Scheduler: enabled}}
			got := builtIntervals(t, cfg, StandardTaskDeps{})
			if iv, ok := got[tc.taskName]; !ok {
				t.Fatalf("%s not registered when its interval is positive", tc.taskName)
			} else if iv != tc.wantInterval {
				t.Fatalf("%s interval = %d, want %d", tc.taskName, iv, tc.wantInterval)
			}

			zero := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true}}}
			gotZero := builtIntervals(t, zero, StandardTaskDeps{})
			if _, ok := gotZero[tc.taskName]; ok {
				t.Fatalf("%s registered with a zero interval; want unregistered", tc.taskName)
			}
		})
	}
}

func TestBuildStandardTasks_CategoryArtGating(t *testing.T) {
	const taskName = "category_art_sync"
	cases := []struct {
		name        string
		interval    int
		withService bool
		wantPresent bool
	}{
		// A nondaily interval exposes accidental use of the daily maintenance cadence.
		{name: "service and interval", interval: 45, withService: true, wantPresent: true},
		{name: "service but zero interval", interval: 0, withService: true, wantPresent: false},
		{name: "interval but nil service", interval: 45, withService: false, wantPresent: false},
		{name: "neither", interval: 0, withService: false, wantPresent: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, CategoryArtIntervalMinutes: tc.interval}},
			}
			var artsvc *categoryart.Service
			if tc.withService {
				artsvc = categoryArtService(t)
			}
			got := builtIntervals(t, cfg, StandardTaskDeps{CategoryArt: artsvc})
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

func TestBuildStandardTasks_CategoryMetadataGating(t *testing.T) {
	const taskName = "category_metadata_sync"
	cases := []struct {
		name        string
		interval    int
		withService bool
		wantPresent bool
	}{
		{name: "service and interval", interval: 75, withService: true, wantPresent: true},
		{name: "service but zero interval", interval: 0, withService: true, wantPresent: false},
		{name: "interval but nil service", interval: 75, withService: false, wantPresent: false},
		{name: "neither", interval: 0, withService: false, wantPresent: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, CategoryMetadataIntervalMinutes: tc.interval}},
			}
			var metasvc *categorymeta.Service
			if tc.withService {
				metasvc = categoryMetadataService(t)
			}
			got := builtIntervals(t, cfg, StandardTaskDeps{CategoryMetadata: metasvc})
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

func TestBuildStandardTasks_EventSubConditionsAreIndependent(t *testing.T) {
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
						Enabled:                          true,
						EventsubReconcileIntervalMinutes: tc.reconcileMin,
						EventsubIntervalMinutes:          tc.snapshotMin,
					},
				},
				ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
			}
			got := builtIntervals(t, cfg, StandardTaskDeps{EventSub: eventsubService(t)})
			if got[taskEventSubReconcileChannels] != tc.wantReconcile {
				t.Fatalf("%s interval = %d, want %d", taskEventSubReconcileChannels, got[taskEventSubReconcileChannels], tc.wantReconcile)
			}
			if got[taskEventSubSnapshot] != tc.wantSnapshot {
				t.Fatalf("%s interval = %d, want %d", taskEventSubSnapshot, got[taskEventSubSnapshot], tc.wantSnapshot)
			}
		})
	}
}

func TestBuildStandardTasks_ConfigTaskBodiesCallExpectedRepoMethods(t *testing.T) {
	baseRepo := newTestRepo(t)
	sentinel := errors.New("sentinel task body error")
	repo := &taskBodyRepo{Repository: baseRepo, err: sentinel}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				Enabled:                               true,
				TokenCleanupIntervalMinutes:           60,
				SessionCleanupIntervalMinutes:         60,
				FetchLogsRetentionDays:                14,
				WebhookEventPayloadRetentionDays:      7,
				EventLogsRetentionDays:                30,
				RecordingWebhookDeliveryRetentionDays: 11,
			},
		},
	}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{}, log)

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
			task, ok := tasks[tc.taskName]
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

func TestBuildStandardTasks_EventSubReconcileTaskListsChannels(t *testing.T) {
	baseRepo := newTestRepo(t)
	sentinel := errors.New("list channels failed")
	repo := &taskBodyRepo{Repository: baseRepo, err: sentinel}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{Enabled: true, EventsubReconcileIntervalMinutes: 15},
		},
	}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{EventSub: eventsubService(t)}, log)

	task := tasks[taskEventSubReconcileChannels]
	err := task.Run(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("reconcile Run() error = %v, want ListChannels sentinel", err)
	}
	if !reflect.DeepEqual(repo.calls, []string{"ListChannels"}) {
		t.Fatalf("calls = %v, want [ListChannels]", repo.calls)
	}
}

func TestBuildStandardTasks_EventSubSnapshotTaskRunsSnapshot(t *testing.T) {
	repo := newTestRepo(t)
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
	cfg := &config.Config{ServerMode: config.ServerModeConfig{Mode: config.ServerModeDirect},
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{Enabled: true, EventsubIntervalMinutes: 10},
		},
	}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{EventSub: esvc}, log)

	if err := tasks[taskEventSubSnapshot].Run(context.Background()); err != nil {
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

func TestBuildStandardTasks_CategoryArtTaskRunsSyncMissing(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-42", Name: "Game 42"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	fakeGames := &schedulerFakeGames{
		byID: map[string]twitch.Game{
			"game-42": {
				BoxArtURL: "https://static-cdn.jtvnw.net/ttv-boxart/game-42-{width}x{height}.jpg",
				IGDBID:    "4242",
			},
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	artsvc := categoryart.New(repo, fakeGames, log)
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{Enabled: true, CategoryArtIntervalMinutes: 45},
		},
	}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{CategoryArt: artsvc}, log)

	if err := tasks["category_art_sync"].Run(ctx); err != nil {
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
	if cat.IGDBID == nil || *cat.IGDBID != "4242" {
		t.Fatalf("IGDBID = %v, want 4242", cat.IGDBID)
	}
}

func TestBuildStandardTasks_CategoryArtTaskQueuesMetadataSync(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-42", Name: "Game 42"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	fakeGames := &schedulerFakeGames{
		byID: map[string]twitch.Game{
			"game-42": {IGDBID: "4242"},
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	artsvc := categoryart.New(repo, fakeGames, log)
	metasvc := categorymeta.New(repo, &schedulerFakeIGDB{}, log)
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				Enabled:                         true,
				CategoryArtIntervalMinutes:      45,
				CategoryMetadataIntervalMinutes: 75,
			},
		},
	}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{
		CategoryArt:      artsvc,
		CategoryMetadata: metasvc,
	}, log)
	if _, err := repo.UpsertTask(ctx, taskCategoryMetadataSync, "metadata", 75*60); err != nil {
		t.Fatalf("seed metadata task row: %v", err)
	}

	if err := tasks[taskCategoryArtSync].Run(ctx); err != nil {
		t.Fatalf("category_art_sync Run(): %v", err)
	}
	task, err := repo.GetTask(ctx, taskCategoryMetadataSync)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", taskCategoryMetadataSync, err)
	}
	if task.NextRunAt == nil {
		t.Fatalf("%s next_run_at is nil; want queued after art sync fills igdb_id", taskCategoryMetadataSync)
	}
}

func TestBuildStandardTasks_CategoryArtTaskDoesNotQueueMetadataForArtOnlyCategory(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	art := "https://static-cdn.jtvnw.net/ttv-boxart/special-{width}x{height}.jpg"
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID:        "special",
		Name:      "Special Category",
		BoxArtURL: &art,
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	fakeGames := &schedulerFakeGames{
		byID: map[string]twitch.Game{
			"special": {BoxArtURL: art},
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	artsvc := categoryart.New(repo, fakeGames, log)
	metasvc := categorymeta.New(repo, &schedulerFakeIGDB{}, log)
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{
				Enabled:                         true,
				CategoryArtIntervalMinutes:      45,
				CategoryMetadataIntervalMinutes: 75,
			},
		},
	}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{
		CategoryArt:      artsvc,
		CategoryMetadata: metasvc,
	}, log)
	if _, err := repo.UpsertTask(ctx, taskCategoryMetadataSync, "metadata", 75*60); err != nil {
		t.Fatalf("seed metadata task row: %v", err)
	}

	if err := tasks[taskCategoryArtSync].Run(ctx); err != nil {
		t.Fatalf("category_art_sync Run(): %v", err)
	}
	task, err := repo.GetTask(ctx, taskCategoryMetadataSync)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", taskCategoryMetadataSync, err)
	}
	if task.NextRunAt != nil {
		t.Fatalf("%s next_run_at = %v, want nil for art-only/no-IGDB sync", taskCategoryMetadataSync, task.NextRunAt)
	}
}

func TestBuildStandardTasks_CategoryMetadataTaskRunsSyncMissing(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	igdbID := "4242"
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID:     "game-42",
		Name:   "Game 42",
		IGDBID: &igdbID,
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	fakeIGDB := &schedulerFakeIGDB{
		byID: map[int64]igdb.Game{
			4242: {Summary: "A useful game description."},
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	metasvc := categorymeta.New(repo, fakeIGDB, log)
	cfg := &config.Config{
		App: config.AppConfig{
			Scheduler: config.SchedulerConfig{Enabled: true, CategoryMetadataIntervalMinutes: 75},
		},
	}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{CategoryMetadata: metasvc}, log)

	if err := tasks["category_metadata_sync"].Run(ctx); err != nil {
		t.Fatalf("category_metadata_sync Run(): %v", err)
	}
	if len(fakeIGDB.calls) != 1 || !reflect.DeepEqual(fakeIGDB.calls[0], []int64{4242}) {
		t.Fatalf("IGDB calls = %v, want [[4242]]", fakeIGDB.calls)
	}
	cat, err := repo.GetCategory(ctx, "game-42")
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if cat.Description == nil || *cat.Description != "A useful game description." {
		t.Fatalf("Description = %v, want fake IGDB description", cat.Description)
	}
}

func TestBuildStandardTasks_RecordingsRetentionGating(t *testing.T) {
	const taskName = "recordings_retention"
	cases := []struct {
		name        string
		interval    int
		withService bool
		wantPresent bool
	}{
		// A nondaily interval exposes accidental use of the daily maintenance cadence.
		{name: "service and interval", interval: 30, withService: true, wantPresent: true},
		{name: "service but zero interval", interval: 0, withService: true, wantPresent: false},
		{name: "interval but nil service", interval: 30, withService: false, wantPresent: false},
		{name: "neither", interval: 0, withService: false, wantPresent: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, RecordingsRetentionIntervalMinutes: tc.interval}},
			}
			var retsvc *retention.Service
			if tc.withService {
				retsvc = retentionService(t)
			}
			got := builtIntervals(t, cfg, StandardTaskDeps{Retention: retsvc})
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

func TestBuildStandardTasks_RecordingsRetentionTaskDeletesExpired(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

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
	// MarkVideoDone uses the real clock, so backdate completion beyond the retention window.
	if _, err := db.ExecContext(ctx, "UPDATE videos SET downloaded_at = datetime('now','-2 hours') WHERE id = ?", vid.ID); err != nil {
		t.Fatalf("backdate completion: %v", err)
	}
	if err := store.Save(ctx, "videos/rec1-part01.mp4", strings.NewReader("data")); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, RecordingsRetentionIntervalMinutes: 30}}}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{Retention: retention.New(repo, mediatest.New(t, repo, store, readyStorage{}, nil), log)}, log)

	if err := tasks["recordings_retention"].Run(ctx); err != nil {
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

type deleteFailStore struct {
	storage.Storage
	err error
}

func (s deleteFailStore) Delete(context.Context, string) error { return s.err }

// TestBuildStandardTasks_RecordingsRetentionTaskPropagatesError prevents storage failures from appearing as successful task runs.
func TestBuildStandardTasks_RecordingsRetentionTaskPropagatesError(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

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
	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, RecordingsRetentionIntervalMinutes: 30}}}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{Retention: retention.New(repo, mediatest.New(t, repo, store, readyStorage{}, nil), log)}, log)

	runErr := tasks["recordings_retention"].Run(ctx)
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

func TestBuildStandardTasksStorageWorkersRequireServiceAndInterval(t *testing.T) {
	for _, taskName := range []string{TaskStorageScan, taskArchivePosters} {
		for _, withService := range []bool{false, true} {
			for _, interval := range []int{0, 7} {
				t.Run(fmt.Sprintf("%s/service=%v/interval=%d", taskName, withService, interval), func(t *testing.T) {
					cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true}}}
					deps := StandardTaskDeps{}
					switch taskName {
					case TaskStorageScan:
						cfg.App.Scheduler.StorageScanIntervalMinutes = interval
						if withService {
							deps.StorageScan = storageScanService(t)
						}
					case taskArchivePosters:
						cfg.App.Scheduler.ArchivePosterIntervalMinutes = interval
						if withService {
							deps.ArchivePosters = archivePosterService(t)
						}
					}
					got := builtIntervals(t, cfg, deps)
					seconds, present := got[taskName]
					want := withService && interval > 0
					if present != want || (present && seconds != int64(interval)*60) {
						t.Fatalf("task registered=%v interval=%d, want registered=%v interval=%d", present, seconds, want, interval*60)
					}
				})
			}
		}
	}
}

func TestBuildStandardTasksUsesStartupConfiguration(t *testing.T) {
	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{
		Enabled: true, TokenCleanupIntervalMinutes: 3, FetchLogsRetentionDays: 7,
	}}}
	repo := &taskBodyRepo{}
	tasks := builtTasks(t, cfg, repo, StandardTaskDeps{}, slog.New(slog.DiscardHandler))
	cfg.App.Scheduler.Enabled = false
	cfg.App.Scheduler.TokenCleanupIntervalMinutes = 5
	cfg.App.Scheduler.FetchLogsRetentionDays = 1
	if tasks["app_token_cleanup"].IntervalSeconds != 3*60 {
		t.Fatal("changing configuration mutated an existing task's interval")
	}
	before := time.Now().AddDate(0, 0, -7)
	if err := tasks["fetch_logs_retention"].Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := time.Now().AddDate(0, 0, -7)
	if repo.fetchCutoff.Before(before) || repo.fetchCutoff.After(after) {
		t.Fatalf("task did not retain its startup retention window: %v", repo.fetchCutoff)
	}
	if next := BuildStandardTasks(cfg, nil, StandardTaskDeps{}, slog.New(slog.DiscardHandler)); len(next) != 0 {
		t.Fatal("a fresh build did not apply the changed configuration")
	}
}

func TestBuildStandardTasksEventSubRequiresActiveMode(t *testing.T) {
	esvc := eventsubService(t)
	for _, mode := range []string{"", config.ServerModeOff, config.ServerModePoll, config.ServerModeDirect, config.ServerModeRelay} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{
				App:        config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, EventsubIntervalMinutes: 10, EventsubReconcileIntervalMinutes: 15}},
				ServerMode: config.ServerModeConfig{Mode: mode},
			}
			want := map[string]int64{}
			if mode == config.ServerModeDirect || mode == config.ServerModeRelay {
				want[taskEventSubSnapshot], want[taskEventSubReconcileChannels] = 10*60, 15*60
			}
			assertExactTasks(t, builtIntervals(t, cfg, StandardTaskDeps{EventSub: esvc}), want)
			assertExactTasks(t, builtIntervals(t, cfg, StandardTaskDeps{}), map[string]int64{})
		})
	}
}

func TestBuildStandardTasksPlaybackCacheHandler(t *testing.T) {
	var calls int
	boom := errors.New("playback maintenance failed")
	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true}}}
	tasks := builtTasks(t, cfg, nil, StandardTaskDeps{PlaybackCacheReconcile: func(context.Context) error {
		calls++
		return boom
	}}, slog.New(slog.DiscardHandler))
	if len(tasks) != 1 || calls != 0 {
		t.Fatalf("builder ran maintenance or selected extra tasks: tasks=%v calls=%d", tasks, calls)
	}
	if err := tasks[taskPlaybackCacheReconcile].Run(t.Context()); !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("maintenance Run: calls=%d, error=%v", calls, err)
	}
}

type readyStorage struct{}

func (readyStorage) Verify(context.Context) error { return nil }
