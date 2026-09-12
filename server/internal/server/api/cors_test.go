package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/trpcgo/trpc"
	"github.com/go-chi/chi/v5"
)

const (
	corsDashboardOrigin = "https://dashboard.example"
	corsChannel         = "cors-channel"
)

type corsHarness struct {
	router *chi.Mux
	repo   repository.Repository
	store  storage.Storage
	bus    *eventbus.Buses
	viewer *http.Cookie
	admin  *http.Cookie
}

// newCORSHarness builds the real router for a split deployment with a local
// store.
func newCORSHarness(t *testing.T) *corsHarness {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionMgr, err := session.NewManager(repo, "cors-harness-session-secret-0123456789abcdef", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{
			HMACSecret:     routerWebhookSecret,
			PublicBaseURL:  "https://api.example",
			CallbackURL:    "https://api.example/api/v1/auth/twitch/callback",
			FrontendURL:    corsDashboardOrigin,
			TrustedOrigins: []string{corsDashboardOrigin},
		},
		ServerMode: config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset},
	}
	bus := eventbus.New()
	eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
	// Attach the storage like main does; a router without attached storage
	// answers every missing file as an outage instead of a 404.
	recordings := NewRecordingServices(cfg, repo, store, bus, log)
	if _, err := recordings.StorageHealth.Attach(context.Background()); err != nil {
		t.Fatalf("attach storage: %v", err)
	}
	router, closeTRPC := SetupRouter(cfg, repo, sessionMgr, nil, store, nil, nil, bus, eventProcessor, nil, nil, log, recordings)
	if closeTRPC != nil {
		t.Cleanup(func() {
			if err := closeTRPC(); err != nil {
				t.Errorf("close tRPC router: %v", err)
			}
		})
	}
	contracttest.SeedUserChannel(t, context.Background(), repo, "cors-seed-user", corsChannel)
	return &corsHarness{
		router: router,
		repo:   repo,
		store:  store,
		bus:    bus,
		viewer: mintSessionCookie(t, repo, sessionMgr, "cors-viewer", "viewer"),
		admin:  mintSessionCookie(t, repo, sessionMgr, "cors-admin", "admin"),
	}
}

// seedRecording creates a finished recording with parts part rows. Only the
// part indexes listed in present get media in the store.
func (h *corsHarness) seedRecording(t *testing.T, jobID string, parts int, present ...int) int64 {
	t.Helper()
	ctx := context.Background()
	v, err := h.repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: jobID, DisplayName: corsChannel,
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: corsChannel, RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo %s: %v", jobID, err)
	}
	for i := 1; i <= parts; i++ {
		if _, err := h.repo.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: int32(i), Filename: fmt.Sprintf("%s-part%02d.mp4", jobID, i),
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatalf("CreateVideoPart %s: %v", jobID, err)
		}
	}
	if err := h.repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoDone %s: %v", jobID, err)
	}
	for _, i := range present {
		key := storagekeys.Video(fmt.Sprintf("%s-part%02d.mp4", jobID, i))
		if err := h.store.Save(ctx, key, strings.NewReader("video-bytes")); err != nil {
			t.Fatalf("save %s: %v", key, err)
		}
	}
	return v.ID
}

type corsOrigin struct {
	name      string
	origin    string
	wantAllow string
}

var corsOrigins = []corsOrigin{
	{"trusted origin", corsDashboardOrigin, corsDashboardOrigin},
	{"untrusted origin", "https://evil.example", ""},
	{"same origin", "", ""},
}

func (h *corsHarness) do(t *testing.T, method, path, body string, cookie *http.Cookie, o corsOrigin, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if o.origin != "" {
		req.Header.Set("Origin", o.origin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	if rr.Code != wantStatus {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, rr.Code, wantStatus, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != o.wantAllow {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, o.wantAllow)
	}
	wantCredentials := ""
	if o.wantAllow != "" {
		wantCredentials = "true"
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != wantCredentials {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want %q", got, wantCredentials)
	}
	return rr
}

func TestStreamRoutesExposeStatusToCrossOriginProbe(t *testing.T) {
	// The watch page reads the stream status through a credentialed HEAD
	// probe; without the allow headers the browser sees a network error.
	h := newCORSHarness(t)
	ctx := context.Background()
	present := h.seedRecording(t, "cors-present", 1, 1)
	removed := h.seedRecording(t, "cors-removed", 1, 1)
	if err := h.repo.SoftDeleteVideo(ctx, removed, repository.DeletionKindManual); err != nil {
		t.Fatalf("SoftDeleteVideo: %v", err)
	}
	// Part 1 stays so the 404 does not tombstone the recording; it runs last
	// because each 404 kicks the missing marker in the background.
	missing := h.seedRecording(t, "cors-missing", 2, 1)

	routes := []struct {
		name   string
		path   string
		status int
	}{
		{"present file", fmt.Sprintf("/api/v1/videos/%d/parts/1/stream", present), http.StatusOK},
		{"removed recording", fmt.Sprintf("/api/v1/videos/%d/parts/1/stream", removed), http.StatusGone},
		{"missing file", fmt.Sprintf("/api/v1/videos/%d/parts/2/stream", missing), http.StatusNotFound},
	}
	for _, route := range routes {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			for _, o := range corsOrigins {
				t.Run(route.name+"/"+method+"/"+o.name, func(t *testing.T) {
					h.do(t, method, route.path, "", h.viewer, o, route.status)
				})
			}
		}
	}
}

func TestTRPCRoutesAreReadableCrossOrigin(t *testing.T) {
	h := newCORSHarness(t)

	for _, o := range corsOrigins {
		t.Run("query/"+o.name, func(t *testing.T) {
			h.do(t, http.MethodGet, "/trpc/system.listInvites", "", h.admin, o, http.StatusOK)
		})
	}
	// A cookie-bearing POST without Origin is refused by CSRF, so the
	// same-origin case carries the API origin, which CORS trusts as well.
	for _, tc := range []struct {
		corsOrigin
		status int
	}{
		{corsOrigins[0], http.StatusNotFound},
		{corsOrigins[1], http.StatusForbidden},
		{corsOrigin{"same origin", "https://api.example", "https://api.example"}, http.StatusNotFound},
	} {
		t.Run("mutation/"+tc.name, func(t *testing.T) {
			h.do(t, http.MethodPost, "/trpc/system.revokeInvite", `{"id":99999}`, h.admin, tc.corsOrigin, tc.status)
		})
	}

	t.Run("subscription resume preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/trpc/stream.live", nil)
		req.Header.Set("Origin", corsDashboardOrigin)
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)
		req.Header.Set("Access-Control-Request-Headers", "last-event-id")
		rr := httptest.NewRecorder()
		h.router.ServeHTTP(rr, req)
		if rr.Code < 200 || rr.Code >= 300 {
			t.Fatalf("preflight status = %d, want 2xx", rr.Code)
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != corsDashboardOrigin {
			t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, corsDashboardOrigin)
		}
		if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("Access-Control-Allow-Credentials = %q, want true", got)
		}
		allowed := strings.Split(rr.Header().Get("Access-Control-Allow-Headers"), ",")
		if !slices.ContainsFunc(allowed, func(v string) bool { return strings.EqualFold(strings.TrimSpace(v), "Last-Event-Id") }) {
			t.Fatalf("Access-Control-Allow-Headers = %q, want Last-Event-Id so EventSource can resume", allowed)
		}
	})

	t.Run("other methods are refused by the router", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/trpc/system.listInvites", nil)
		req.AddCookie(h.admin)
		rr := httptest.NewRecorder()
		h.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("PUT /trpc = %d, want 405 from the per-method mount", rr.Code)
		}
		for _, method := range trpc.Methods() {
			if !slices.Contains(rr.Header().Values("Allow"), method) {
				t.Fatalf("Allow = %v, want %s", rr.Header().Values("Allow"), method)
			}
		}
	})
}

func TestTRPCSubscriptionAcceptsResumeHeader(t *testing.T) {
	// trpcgo merges Last-Event-Id into input as lastEventId. stream.live is a
	// void subscription: it must accept that reconnect input and deliver new
	// events, but it never replays missed ones.
	for _, tc := range []struct {
		name        string
		lastEventID string
	}{
		{"initial connection", ""},
		{"resume header", "event-7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCORSHarness(t)
			server := httptest.NewServer(h.router)
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/trpc/stream.live", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(h.viewer)
			req.Header.Set("Origin", corsDashboardOrigin)
			req.Header.Set("Accept", "text/event-stream")
			if tc.lastEventID != "" {
				req.Header.Set("Last-Event-Id", tc.lastEventID)
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatalf("open subscription: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("subscription status = %d, want 200", resp.StatusCode)
			}
			for header, want := range map[string]string{
				"Content-Type":                     "text/event-stream",
				"Access-Control-Allow-Origin":      corsDashboardOrigin,
				"Access-Control-Allow-Credentials": "true",
			} {
				if got := resp.Header.Get(header); got != want {
					t.Fatalf("%s = %q, want %q", header, got, want)
				}
			}

			scanner := bufio.NewScanner(resp.Body)
			if event, data := readCORSSSEEvent(t, scanner); event != "connected" || !json.Valid([]byte(data)) {
				t.Fatalf("first SSE event = %q, data = %q; want tRPC connected frame with JSON data", event, data)
			}
			// The connected frame is flushed after the resolver subscribes, so
			// publishing here cannot race the subscription and needs no sleep.
			want := eventbus.StreamLiveEvent{
				BroadcasterID: corsChannel, BroadcasterLogin: "cors-live",
				StreamID: tc.name, StartedAt: time.Date(2026, time.September, 8, 0, 0, 0, 0, time.UTC),
			}
			h.bus.StreamLive.Publish(want)
			event, data := readCORSSSEEvent(t, scanner)
			if event != "message" {
				t.Fatalf("SSE event = %q, data = %q; want a live message", event, data)
			}
			var got eventbus.StreamLiveEvent
			if err := json.Unmarshal([]byte(data), &got); err != nil {
				t.Fatalf("decode live event: %v; data: %s", err, data)
			}
			if got != want {
				t.Fatalf("live event = %+v, want %+v", got, want)
			}
		})
	}
}

func readCORSSSEEvent(t *testing.T, scanner *bufio.Scanner) (string, string) {
	t.Helper()
	event := "message"
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			return event, strings.Join(data, "\n")
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	t.Fatalf("stream ended before a complete SSE event: %v", scanner.Err())
	return "", ""
}

func TestRoutedMethodsListsExplicitRoutesOnly(t *testing.T) {
	ok := func(http.ResponseWriter, *http.Request) {}
	r := chi.NewRouter()
	r.Get("/a", ok)
	r.Head("/a", ok)
	r.HandleFunc("/catch/*", ok)
	r.Route("/sub", func(r chi.Router) {
		r.Delete("/x", ok)
		r.Post("/y", ok)
		r.Post("/z", ok)
	})
	r.Mount("/m", http.HandlerFunc(ok))

	got := routedMethods(r)

	want := []string{http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodPost}
	if !slices.Equal(got, want) {
		t.Fatalf("routedMethods = %v, want %v", got, want)
	}
}

func TestCORSPolicyFollowsTheRouter(t *testing.T) {
	h := newCORSHarness(t)
	preflight := func(method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/videos/1/parts/1/stream", nil)
		req.Header.Set("Origin", corsDashboardOrigin)
		req.Header.Set("Access-Control-Request-Method", method)
		rr := httptest.NewRecorder()
		h.router.ServeHTTP(rr, req)
		return rr
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		if got := preflight(method).Header().Get("Access-Control-Allow-Origin"); got != corsDashboardOrigin {
			t.Fatalf("preflight %s: Access-Control-Allow-Origin = %q, want %q", method, got, corsDashboardOrigin)
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if got := preflight(method).Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("preflight %s: Access-Control-Allow-Origin = %q, want none for a method no route serves", method, got)
		}
	}
}
