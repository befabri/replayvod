package recordingwebhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/videodownload"
)

func newTestDispatcher(store *fakeRepo) *Dispatcher {
	return &Dispatcher{
		svc:                        New(store, nil),
		store:                      store,
		client:                     newDeliveryClient(),
		log:                        slog.New(slog.NewTextHandler(io.Discard, nil)),
		signURL:                    videodownload.NewSigner("test-hmac-secret", "https://app.example", time.Hour).PartURLUntil,
		capDownloadURLsAtRetention: true,
		sem:                        make(chan struct{}, defaultConcurrency),
		attempts:                   3,
		timeout:                    200 * time.Millisecond,
		backoff:                    time.Millisecond,
		maxBackoff:                 20 * time.Millisecond,
		pollInterval:               time.Hour,
		staleTimeout:               time.Minute,
		drainTimeout:               time.Second,
	}
}

func completedStore() *fakeRepo {
	return &fakeRepo{
		video: sampleVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", SizeBytes: 600, DurationSeconds: 1800},
		},
		channel: &repository.Channel{BroadcasterLogin: "speedy", BroadcasterName: "Speedy"},
	}
}

func verifySignature(secret string, h http.Header, body []byte) bool {
	id := h.Get(HeaderID)
	ts := h.Get(HeaderTimestamp)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	mac.Write([]byte(ts))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(h.Get(HeaderSignature)), []byte(expected))
}

func enqueueTerminal(t *testing.T, store *fakeRepo, event string, videoID int64, due time.Time) repository.RecordingWebhookDelivery {
	t.Helper()
	input := NewTerminalDeliveryInput(event, videoID, due)
	row, err := store.CreateRecordingWebhookDelivery(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}
	return *row
}

func claimOne(t *testing.T, store *fakeRepo) repository.RecordingWebhookDelivery {
	t.Helper()
	rows, err := store.ClaimDueRecordingWebhookDeliveries(context.Background(), time.Now().UTC(), 1)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one claimed row, got %d", len(rows))
	}
	return rows[0]
}

func deliverySnapshot(t *testing.T, store *fakeRepo, id int64) repository.RecordingWebhookDelivery {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	i, err := store.deliveryIndex(id)
	if err != nil {
		t.Fatalf("delivery %d not found: %v", id, err)
	}
	return *cloneDelivery(store.deliveries[i])
}

func TestNewDispatcher_setsProductionDefaultsAndOptionalSigner(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	d := NewDispatcher(repo, nil, log)
	if d == nil {
		t.Fatal("NewDispatcher returned nil")
	}
	if d.svc == nil || d.store != repo || d.client == nil || d.log == nil {
		t.Fatalf("dispatcher dependencies not wired: svc=%v store=%T client=%v log=%v", d.svc, d.store, d.client, d.log)
	}
	if d.signURL != nil {
		t.Fatal("nil signer should leave signed part URLs disabled")
	}
	if cap(d.sem) != defaultConcurrency {
		t.Fatalf("semaphore cap = %d, want %d", cap(d.sem), defaultConcurrency)
	}
	if d.attempts != defaultAttempts ||
		d.timeout != defaultTimeout ||
		d.backoff != defaultBackoff ||
		d.maxBackoff != defaultMaxBackoff ||
		d.pollInterval != defaultPollInterval ||
		d.staleTimeout != defaultStaleTimeout ||
		d.drainTimeout != defaultDrainTimeout {
		t.Fatalf("dispatcher defaults drifted: %+v", d)
	}
	if d.client.Timeout != defaultTimeout {
		t.Fatalf("HTTP timeout = %v, want %v", d.client.Timeout, defaultTimeout)
	}
	if err := d.client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect guard = %v, want http.ErrUseLastResponse", err)
	}

	signer := videodownload.NewSigner("test-hmac-secret", "https://app.example", time.Hour)
	withSigner := NewDispatcher(repo, signer, nil)
	if withSigner.log == nil {
		t.Fatal("nil logger should be replaced with the default logger")
	}
	if withSigner.signURL == nil {
		t.Fatal("non-nil signer should wire part URL signing")
	}
	if !withSigner.capDownloadURLsAtRetention {
		t.Fatal("retention URL cap should default on")
	}
	if got := withSigner.signURL(42, 1, nil); !strings.HasPrefix(got, "https://app.example/api/v1/videos/42/parts/1/download?") {
		t.Fatalf("signed part URL = %q", got)
	}
}

func TestDeliverClaimed_signsRawBodySendsShapeAndPersistsDelivered(t *testing.T) {
	const secret = "top-secret"
	type captured struct {
		sigOK   bool
		event   string
		payload Payload
		ctype   string
	}
	var got captured
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got.sigOK = verifySignature(secret, r.Header, body)
		got.event = r.Header.Get(HeaderEvent)
		got.ctype = r.Header.Get("Content-Type")
		_ = json.Unmarshal(body, &got.payload)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  secret,
	}
	d := newTestDispatcher(store)
	enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	d.deliverClaimed(context.Background(), claimOne(t, store))

	mu.Lock()
	defer mu.Unlock()
	if !got.sigOK {
		t.Fatal("receiver could not verify the HMAC signature over the raw body")
	}
	if got.event != EventCompleted {
		t.Fatalf("event header = %q, want %q", got.event, EventCompleted)
	}
	if got.ctype != "application/json" {
		t.Fatalf("content-type = %q", got.ctype)
	}
	if got.payload.VideoID != 42 || got.payload.Event != EventCompleted {
		t.Fatalf("payload shape wrong: %+v", got.payload)
	}
	if len(got.payload.Parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(got.payload.Parts))
	}
	wantPrefix := "https://app.example/api/v1/videos/42/parts/1/download?"
	if dl := got.payload.Parts[0].DownloadURL; !strings.HasPrefix(dl, wantPrefix) {
		t.Fatalf("part download URL = %q, want prefix %q", dl, wantPrefix)
	}
	recent, err := d.RecentDeliveries(context.Background())
	if err != nil {
		t.Fatalf("RecentDeliveries: %v", err)
	}
	if len(recent) != 1 || recent[0].Outcome != OutcomeDelivered || recent[0].Status != http.StatusOK || recent[0].Attempts != 1 {
		t.Fatalf("delivery row not marked delivered: %+v", recent)
	}
}

func TestDeliverClaimed_retriesWithDurableBackoffThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	d.backoff = 5 * time.Millisecond
	row := enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	before := time.Now().UTC()
	d.deliverClaimed(context.Background(), claimOne(t, store))
	after := time.Now().UTC()
	recent, _ := d.RecentDeliveries(context.Background())
	if recent[0].Outcome != OutcomePending || recent[0].Attempts != 1 || recent[0].Status != http.StatusInternalServerError {
		t.Fatalf("first failure should persist pending retry, got %+v", recent[0])
	}
	stored := deliverySnapshot(t, store, row.ID)
	if stored.NextAttemptAt.Before(before.Add(d.backoff)) || stored.NextAttemptAt.After(after.Add(d.backoff)) {
		t.Fatalf("next attempt = %v, want within [%v, %v]", stored.NextAttemptAt, before.Add(d.backoff), after.Add(d.backoff))
	}

	if wait := time.Until(stored.NextAttemptAt.Add(time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	d.deliverClaimed(context.Background(), claimOne(t, store))
	recent, _ = d.RecentDeliveries(context.Background())
	if calls.Load() != 2 || recent[0].Outcome != OutcomeDelivered || recent[0].Attempts != 2 {
		t.Fatalf("second attempt should deliver, calls=%d recent=%+v", calls.Load(), recent[0])
	}
}

// TestDeliverClaimed_freezesPartsAndRegeneratesURLAcrossRetentionRetry pins the
// whole retention-race design: the part metadata is frozen on the first attempt
// while the parts exist, so a retry after retention deleted those parts still
// carries the real part (frozen metadata survives), AND the signed download URL
// is re-minted per attempt rather than frozen, so a late retry never ships a
// stale URL. The signer here stamps a monotonically advancing token so a frozen
// URL is observably distinct from a regenerated one.
func TestDeliverClaimed_freezesPartsAndRegeneratesURLAcrossRetentionRetry(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var bodies []Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p Payload
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		bodies = append(bodies, p)
		mu.Unlock()
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError) // fail first so it retries
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	d.backoff = 5 * time.Millisecond
	var token atomic.Int64
	d.signURL = func(videoID int64, partIndex int32, _ *time.Time) string {
		return fmt.Sprintf("https://app.example/v/%d/p/%d?tok=%d", videoID, partIndex, token.Add(1))
	}
	row := enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	// First attempt fails, but freezes the part metadata while the part exists.
	d.deliverClaimed(context.Background(), claimOne(t, store))
	if deliverySnapshot(t, store, row.ID).FrozenParts == "" {
		t.Fatal("first attempt did not freeze the part metadata onto the delivery row")
	}

	// Retention deletes the recording's parts before the receiver recovers.
	store.mu.Lock()
	store.parts = nil
	store.mu.Unlock()

	stored := deliverySnapshot(t, store, row.ID)
	if wait := time.Until(stored.NextAttemptAt.Add(time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	d.deliverClaimed(context.Background(), claimOne(t, store))

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("captured %d bodies, want 2", len(bodies))
	}
	// Both attempts carry the real part (frozen metadata survives retention)...
	for i, b := range bodies {
		if len(b.Parts) != 1 {
			t.Fatalf("attempt %d parts = %d, want 1 (a live rebuild after retention would send 0)", i+1, len(b.Parts))
		}
		if b.Parts[0].SizeBytes != 600 || b.Parts[0].PartIndex != 1 {
			t.Fatalf("attempt %d part = %+v, want the frozen index-1/600-byte part", i+1, b.Parts[0])
		}
		if b.Parts[0].DownloadURL == "" {
			t.Fatalf("attempt %d part has no download_url; URL must be regenerated from the frozen part", i+1)
		}
	}
	// ...but the URL is regenerated per attempt, not frozen (distinct token).
	if bodies[0].Parts[0].DownloadURL == bodies[1].Parts[0].DownloadURL {
		t.Fatalf("download_url frozen across attempts (%q); want re-minted per attempt", bodies[0].Parts[0].DownloadURL)
	}
}

// TestDeliverClaimed_freezesPayloadBeforeConfigGate pins that the snapshot runs
// before the disabled/incomplete-config gate. A delivery whose webhook is
// disabled at its first attempt is still failed, but its body must already be
// frozen so a later manual retry (after the operator fixes the config) resends
// the real parts instead of rebuilding from parts retention may have deleted.
func TestDeliverClaimed_freezesPayloadBeforeConfigGate(t *testing.T) {
	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: false, // disabled at this first attempt
		RecordingWebhookURL:     "https://receiver.example",
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	row := enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	d.deliverClaimed(context.Background(), claimOne(t, store))

	snap := deliverySnapshot(t, store, row.ID)
	if snap.Status != repository.RecordingWebhookDeliveryFailed {
		t.Fatalf("status = %q, want failed for a disabled webhook", snap.Status)
	}
	if snap.FrozenParts == "" {
		t.Fatal("parts not frozen before the config gate; a disabled-config delivery would lose its parts on a later retry")
	}
}

func TestDeliverClaimed_freezePersistFailureRetriesWithoutPost(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.freezePartsErr = errors.New("db unavailable")
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	row := enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	d.deliverClaimed(context.Background(), claimOne(t, store))

	if calls.Load() != 0 {
		t.Fatalf("delivery POSTed despite failing to persist frozen parts, calls=%d", calls.Load())
	}
	snap := deliverySnapshot(t, store, row.ID)
	if snap.Status != repository.RecordingWebhookDeliveryPending {
		t.Fatalf("status = %q, want pending retry after frozen-parts persist failure", snap.Status)
	}
	if snap.FrozenParts != "" {
		t.Fatalf("frozen_parts = %q, want empty after injected persist failure", snap.FrozenParts)
	}
	if !strings.Contains(snap.LastError, "persist frozen parts") {
		t.Fatalf("last_error = %q, want frozen-parts persist cause", snap.LastError)
	}
}

func TestDeliverClaimed_doesNotRetryPermanentRejection(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	d.deliverClaimed(context.Background(), claimOne(t, store))

	recent, _ := d.RecentDeliveries(context.Background())
	if calls.Load() != 1 || recent[0].Outcome != OutcomeRejected || recent[0].Attempts != 1 {
		t.Fatalf("4xx should be one rejected attempt, calls=%d recent=%+v", calls.Load(), recent[0])
	}
}

func TestDeliverClaimed_disabledDoesNotPostAndMarksFailed(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: false,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	d.deliverClaimed(context.Background(), claimOne(t, store))

	recent, _ := d.RecentDeliveries(context.Background())
	if calls.Load() != 0 || recent[0].Outcome != OutcomeFailed {
		t.Fatalf("disabled webhook should not POST and should mark failed, calls=%d recent=%+v", calls.Load(), recent[0])
	}
}

func TestDeliverClaimed_missingVideoMarksFailedWithoutPost(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.videoErr = repository.ErrNotFound
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	d.deliverClaimed(context.Background(), claimOne(t, store))

	recent, err := d.RecentDeliveries(context.Background())
	if err != nil {
		t.Fatalf("RecentDeliveries: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("missing video should not be posted, calls=%d", calls.Load())
	}
	if len(recent) != 1 || recent[0].Outcome != OutcomeFailed || recent[0].Status != 0 {
		t.Fatalf("missing video should mark delivery failed, got %+v", recent)
	}
	if !strings.Contains(recent[0].Error, repository.ErrNotFound.Error()) {
		t.Fatalf("failure should record the not-found cause, got %q", recent[0].Error)
	}
}

func TestStart_pollsPendingDeliveryWithoutBus(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())
	d := newTestDispatcher(store)

	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx, nil)
	waitFor(t, func() bool { return calls.Load() == 1 })
	cancel()
	d.Wait()
}

func TestWait_cancelsInflightDeliveriesAfterDrainTimeout(t *testing.T) {
	d := newTestDispatcher(completedStore())
	d.drainTimeout = 5 * time.Millisecond
	d.deliverCtx, d.deliverCancel = context.WithCancel(context.Background())
	defer d.deliverCancel()

	stopped := make(chan struct{})
	close(stopped)
	d.stopped = stopped

	started := make(chan struct{})
	d.wg.Go(func() {
		close(started)
		<-d.deliverCtx.Done()
	})
	<-started

	done := make(chan struct{})
	start := time.Now()
	go func() {
		d.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		d.deliverCancel()
		t.Fatal("Wait did not cancel an in-flight delivery after the drain timeout")
	}
	if d.deliverCtx.Err() == nil {
		t.Fatal("Wait returned without cancelling the delivery context")
	}
	if elapsed := time.Since(start); elapsed < d.drainTimeout {
		t.Fatalf("Wait returned before the drain timeout elapsed: %v < %v", elapsed, d.drainTimeout)
	}
}

func TestStart_busEventWakesPoller(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	bus := eventbus.New()
	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx, bus)
	if bus.RecordingTerminal.Count() == 0 {
		t.Fatal("Start did not subscribe synchronously")
	}

	enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())
	bus.RecordingTerminal.Publish(eventbus.RecordingTerminalEvent{VideoID: 42, Kind: eventbus.RecordingCompleted})
	waitFor(t, func() bool { return calls.Load() == 1 })
	cancel()
	d.Wait()
}

func TestSendTest_postsSignedTestPayloadAndPersistsHistory(t *testing.T) {
	const secret = "test-secret"
	type captured struct {
		event   string
		payload Payload
		sigOK   bool
	}
	var got captured
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got.event = r.Header.Get(HeaderEvent)
		got.sigOK = verifySignature(secret, r.Header, body)
		_ = json.Unmarshal(body, &got.payload)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: false,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  secret,
	}
	d := newTestDispatcher(store)

	res := d.SendTest(context.Background())
	if !res.OK || res.Status != http.StatusOK {
		t.Fatalf("SendTest result = %+v, want ok 200", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.event != EventTest {
		t.Fatalf("event header = %q, want %q", got.event, EventTest)
	}
	if !got.payload.Test || got.payload.Event != EventTest {
		t.Fatalf("test payload shape wrong: %+v", got.payload)
	}
	if !got.sigOK {
		t.Fatal("test delivery must be signed like a real one")
	}
	recent, _ := d.RecentDeliveries(context.Background())
	if len(recent) != 1 || !recent[0].Test || recent[0].Outcome != OutcomeDelivered {
		t.Fatalf("test delivery not logged correctly: %+v", recent)
	}
}

// TestSendTest_preClaimedRowIsNotClaimedByPoller is the regression guard for the
// SendTest double-delivery race: SendTest creates its row already claimed
// ('delivering') and POSTs it synchronously, so the poller must never also claim
// and re-POST it. Here we simulate that row mid-send (created, not yet marked
// terminal) and prove a poll drain delivers nothing.
func TestSendTest_preClaimedRowIsNotClaimedByPoller(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		posts.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	d.deliverCtx = context.Background() // drainDue spawns deliveries on deliverCtx

	input, err := newTestDeliveryInput(time.Now().UTC())
	if err != nil {
		t.Fatalf("newTestDeliveryInput: %v", err)
	}
	if _, err := d.store.CreateClaimedRecordingWebhookDelivery(context.Background(), input); err != nil {
		t.Fatalf("CreateClaimedRecordingWebhookDelivery: %v", err)
	}

	// The poller drains: the pre-claimed ('delivering') row must not be claimed,
	// so no POST happens. A regression (creating the row 'pending') would let the
	// poller claim and double-send it.
	d.drainDue(context.Background())
	d.wg.Wait()

	if got := posts.Load(); got != 0 {
		t.Fatalf("poller delivered a pre-claimed row; double-send not prevented (posts=%d)", got)
	}
}

func TestSendTest_noURLIsAnError(t *testing.T) {
	store := completedStore()
	store.settings = &repository.ServerSettings{RecordingWebhookURL: ""}
	d := newTestDispatcher(store)
	if res := d.SendTest(context.Background()); res.OK || res.Error == "" {
		t.Fatalf("SendTest with no URL should fail with a message, got %+v", res)
	}
}

func TestSendTest_seedsMissingSecretBeforePosting(t *testing.T) {
	var signature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature = r.Header.Get(HeaderSignature)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{RecordingWebhookURL: srv.URL}
	d := newTestDispatcher(store)

	res := d.SendTest(context.Background())
	if !res.OK {
		t.Fatalf("SendTest: %+v", res)
	}
	if store.settings.RecordingWebhookSecret == "" {
		t.Fatal("SendTest should seed a missing signing secret")
	}
	if signature == "" || signature == "sha256="+strings.Repeat("0", 64) {
		t.Fatalf("signature was not set correctly: %q", signature)
	}
}

func TestDeliver_doesNotFollowRedirect(t *testing.T) {
	var internalHit atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHit.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer internal.Close()

	var redirected atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		http.Redirect(w, &http.Request{}, internal.URL, http.StatusFound)
	}))
	defer srv.Close()

	store := completedStore()
	store.settings = &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     srv.URL,
		RecordingWebhookSecret:  "s",
	}
	d := newTestDispatcher(store)
	enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())

	d.deliverClaimed(context.Background(), claimOne(t, store))

	if internalHit.Load() != 0 {
		t.Fatal("redirect was followed to the internal host; CheckRedirect must refuse it")
	}
	if redirected.Load() == 0 {
		t.Fatal("expected the configured receiver to be hit at least once")
	}
	recent, _ := d.RecentDeliveries(context.Background())
	if len(recent) != 1 || recent[0].Outcome != OutcomeRejected {
		t.Fatalf("redirect should record one rejected delivery, got %+v", recent)
	}
}

func TestRetryDelivery_marksDueAndWakes(t *testing.T) {
	store := completedStore()
	d := newTestDispatcher(store)
	d.wakeCh = make(chan struct{}, 1)
	row := enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())
	claimed := claimOne(t, store)
	if err := store.MarkRecordingWebhookDeliveryFinal(
		context.Background(),
		claimed.ID,
		repository.RecordingWebhookDeliveryFailed,
		http.StatusInternalServerError,
		"boom",
		time.Now().Add(time.Hour),
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("MarkRecordingWebhookDeliveryFinal: %v", err)
	}

	retried, err := d.RetryDelivery(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("RetryDelivery: %v", err)
	}
	if retried.Outcome != OutcomePending || retried.Attempts != 0 || retried.Status != 0 {
		t.Fatalf("retry should reset the row to fresh pending state, got %+v", retried)
	}
	select {
	case <-d.wakeCh:
	default:
		t.Fatal("RetryDelivery should wake the poller after re-queueing the row")
	}
	rows, err := store.ClaimDueRecordingWebhookDeliveries(context.Background(), time.Now().UTC(), 1)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != row.ID || rows[0].Attempts != 1 {
		t.Fatalf("retry should make row due now, got %+v", rows)
	}
}

func TestRetryDelivery_rejectsNonRetryableRows(t *testing.T) {
	store := completedStore()
	d := newTestDispatcher(store)
	pending := enqueueTerminal(t, store, EventCompleted, 42, time.Now().UTC())
	if _, err := d.RetryDelivery(context.Background(), pending.ID); !errors.Is(err, ErrDeliveryNotRetryable) {
		t.Fatalf("retry pending row err = %v, want ErrDeliveryNotRetryable", err)
	}

	delivered := claimOne(t, store)
	if err := store.MarkRecordingWebhookDeliveryDelivered(context.Background(), delivered.ID, http.StatusOK, time.Now().UTC()); err != nil {
		t.Fatalf("MarkRecordingWebhookDeliveryDelivered: %v", err)
	}
	if _, err := d.RetryDelivery(context.Background(), delivered.ID); !errors.Is(err, ErrDeliveryNotRetryable) {
		t.Fatalf("retry delivered row err = %v, want ErrDeliveryNotRetryable", err)
	}
}

func TestBackoffForAttempt_doublesAndCaps(t *testing.T) {
	cases := []struct {
		name       string
		backoff    time.Duration
		maxBackoff time.Duration
		attempt    int
		want       time.Duration
	}{
		{"zero attempt uses base", 5 * time.Millisecond, 100 * time.Millisecond, 0, 5 * time.Millisecond},
		{"first attempt uses base", 5 * time.Millisecond, 100 * time.Millisecond, 1, 5 * time.Millisecond},
		{"second attempt doubles once", 5 * time.Millisecond, 100 * time.Millisecond, 2, 10 * time.Millisecond},
		{"third attempt doubles twice", 5 * time.Millisecond, 100 * time.Millisecond, 3, 20 * time.Millisecond},
		{"cap truncates overshoot", 5 * time.Millisecond, 17 * time.Millisecond, 3, 17 * time.Millisecond},
		{"large attempt remains capped", 5 * time.Millisecond, 17 * time.Millisecond, 10, 17 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDispatcher(completedStore())
			d.backoff = tc.backoff
			d.maxBackoff = tc.maxBackoff
			if got := d.backoffForAttempt(tc.attempt); got != tc.want {
				t.Fatalf("backoffForAttempt(%d) = %v, want %v", tc.attempt, got, tc.want)
			}
		})
	}
}

func TestClassifyOutcome(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   DeliveryOutcome
	}{
		{"2xx delivered", 200, nil, OutcomeDelivered},
		{"transport error failed", 0, errSentinel, OutcomeFailed},
		{"5xx exhausted is failed", 503, nil, OutcomeFailed},
		{"429 exhausted is failed", 429, nil, OutcomeFailed},
		{"4xx rejected", 400, nil, OutcomeRejected},
		{"3xx rejected", 302, nil, OutcomeRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyOutcome(tc.status, tc.err); got != tc.want {
				t.Fatalf("classifyOutcome(%d, %v) = %q, want %q", tc.status, tc.err, got, tc.want)
			}
		})
	}
}

var errSentinel = &url.Error{Op: "Post", URL: "https://u:p@host/x", Err: errBare}

type bareErr struct{}

func (bareErr) Error() string { return "connection refused" }

var errBare = bareErr{}

func TestDescribeErr_stripsURLAndCredentials(t *testing.T) {
	got := describeErr(errSentinel)
	if got != "connection refused" {
		t.Fatalf("describeErr = %q, want the bare cause with no URL", got)
	}
	if describeErr(nil) != "" {
		t.Fatal("describeErr(nil) should be empty")
	}
}

func TestSafeURL_dropsCredentialsAndQuery(t *testing.T) {
	got := safeURL("https://user:pass@hooks.example/path?token=secret#frag")
	if got != "https://hooks.example/path" {
		t.Fatalf("safeURL = %q, want scheme://host/path with no creds/query", got)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
