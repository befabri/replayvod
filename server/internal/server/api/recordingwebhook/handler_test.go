package recordingwebhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	svc "github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// fakeStore implements the repository methods the config service uses, modeling
// the real secret semantics: ensure is CAS (seeds only when empty), set rotates
// unconditionally, and the config upsert never touches the secret.
type fakeStore struct {
	settings  *repository.ServerSettings
	getErr    error
	upsertErr error
	ensureErr error
	setErr    error
}

func (f *fakeStore) GetServerSettings(context.Context) (*repository.ServerSettings, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.settings == nil {
		return nil, repository.ErrNotFound
	}
	return f.settings, nil
}

func (f *fakeStore) row() *repository.ServerSettings {
	if f.settings == nil {
		f.settings = &repository.ServerSettings{}
	}
	return f.settings
}

func (f *fakeStore) UpsertRecordingWebhookConfig(_ context.Context, enabled bool, url, events string) (*repository.ServerSettings, error) {
	if f.upsertErr != nil {
		return nil, f.upsertErr
	}
	r := f.row()
	r.RecordingWebhookEnabled = enabled
	r.RecordingWebhookURL = url
	r.RecordingWebhookEvents = events
	return r, nil
}

func (f *fakeStore) EnsureRecordingWebhookSecret(_ context.Context, secret string) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	r := f.row()
	if r.RecordingWebhookSecret == "" {
		r.RecordingWebhookSecret = secret
	}
	return nil
}

func (f *fakeStore) SetRecordingWebhookSecret(_ context.Context, secret string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.row().RecordingWebhookSecret = secret
	return nil
}

// fakeSender is a stand-in dispatcher for the test/deliveries procedures.
type fakeSender struct {
	result    svc.DeliveryResult
	recent    []svc.DeliveryRecord
	recentErr error
	retried   svc.DeliveryRecord
	retryErr  error
}

func (f *fakeSender) SendTest(context.Context) svc.DeliveryResult { return f.result }
func (f *fakeSender) RecentDeliveries(context.Context) ([]svc.DeliveryRecord, error) {
	return f.recent, f.recentErr
}
func (f *fakeSender) RetryDelivery(_ context.Context, _ int64) (svc.DeliveryRecord, error) {
	return f.retried, f.retryErr
}

func newHandler(store *fakeStore) *Handler {
	return NewHandler(svc.New(store, nil), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func newHandlerWithSender(store *fakeStore, s sender) *Handler {
	return NewHandler(svc.New(store, nil), s, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var errHandlerBoom = errors.New("boom")

func requireTRPCCode(t *testing.T, err error, code trpcgo.ErrorCode) {
	t.Helper()
	var trpcErr *trpcgo.Error
	if !errors.As(err, &trpcErr) || trpcErr.Code != code {
		t.Fatalf("error = %v, want trpc %s", err, trpcgo.NameFromCode(code))
	}
}

func TestHandler_Config_emptyEventsSerializeAsArray(t *testing.T) {
	h := newHandler(&fakeStore{settings: &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     "https://hooks.example/x",
		RecordingWebhookSecret:  "s",
	}})
	resp, err := h.Config(context.Background())
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if resp.Events == nil {
		t.Fatal("events must serialize as [] not null")
	}
	if !resp.Enabled || resp.Secret != "s" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandler_Config_internalErrorMapsToInternal(t *testing.T) {
	h := newHandler(&fakeStore{getErr: errHandlerBoom})
	_, err := h.Config(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeInternalServerError)
}

func TestHandler_UpdateConfig_invalidURLMapsToBadRequest(t *testing.T) {
	h := newHandler(&fakeStore{settings: &repository.ServerSettings{}})
	_, err := h.UpdateConfig(context.Background(), RecordingWebhookUpdateConfigInput{
		Enabled: true,
		URL:     "http://public.example/x", // http non-loopback is rejected
		Events:  []string{svc.EventCompleted},
	})
	if err == nil {
		t.Fatal("expected an error for an invalid URL")
	}
	requireTRPCCode(t, err, trpcgo.CodeBadRequest)
}

func TestHandler_UpdateConfig_success(t *testing.T) {
	h := newHandler(&fakeStore{settings: &repository.ServerSettings{}})
	resp, err := h.UpdateConfig(context.Background(), RecordingWebhookUpdateConfigInput{
		Enabled: true,
		URL:     "https://hooks.example/recordings",
		Events:  []string{"recording.completed"},
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !resp.Enabled || resp.URL != "https://hooks.example/recordings" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(resp.Secret) != 64 {
		t.Fatalf("expected an auto-generated secret, got %q", resp.Secret)
	}
}

func TestHandler_UpdateConfig_internalErrorMapsToInternal(t *testing.T) {
	cases := []struct {
		name  string
		store *fakeStore
	}{
		{
			name:  "seed secret",
			store: &fakeStore{settings: &repository.ServerSettings{}, ensureErr: errHandlerBoom},
		},
		{
			name:  "upsert config",
			store: &fakeStore{settings: &repository.ServerSettings{}, upsertErr: errHandlerBoom},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHandler(tc.store)
			_, err := h.UpdateConfig(context.Background(), RecordingWebhookUpdateConfigInput{
				Enabled: true,
				URL:     "https://hooks.example/recordings",
				Events:  []string{svc.EventCompleted},
			})
			requireTRPCCode(t, err, trpcgo.CodeInternalServerError)
		})
	}
}

func TestHandler_RegenerateSecret_rotates(t *testing.T) {
	h := newHandler(&fakeStore{settings: &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     "https://hooks.example/x",
		RecordingWebhookSecret:  "old",
	}})
	resp, err := h.RegenerateSecret(context.Background())
	if err != nil {
		t.Fatalf("RegenerateSecret: %v", err)
	}
	if resp.Secret == "old" || len(resp.Secret) != 64 {
		t.Fatalf("secret = %q, want a fresh 64-char value", resp.Secret)
	}
	if resp.URL != "https://hooks.example/x" {
		t.Fatalf("regenerate must not disturb the URL, got %q", resp.URL)
	}
}

func TestHandler_RegenerateSecret_internalErrorMapsToInternal(t *testing.T) {
	h := newHandler(&fakeStore{
		settings: &repository.ServerSettings{RecordingWebhookSecret: "old"},
		setErr:   errHandlerBoom,
	})
	_, err := h.RegenerateSecret(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeInternalServerError)
}

func TestHandler_TestDelivery_relaysResult(t *testing.T) {
	sender := &fakeSender{result: svc.DeliveryResult{OK: true, Status: 200}}
	h := newHandlerWithSender(&fakeStore{settings: &repository.ServerSettings{}}, sender)
	res, err := h.TestDelivery(context.Background())
	if err != nil {
		t.Fatalf("TestDelivery: %v", err)
	}
	if !res.OK || res.Status != 200 {
		t.Fatalf("unexpected test result: %+v", res)
	}
}

// TestHandler_TestDelivery_noDispatcherIsServiceUnavailable pins that an inert
// dispatcher (a supported feature-off state) surfaces as 503 ServiceUnavailable,
// not a 500 — the latter would wrongly tell the operator a server fault occurred
// instead of "enable the webhook subsystem."
func TestHandler_TestDelivery_noDispatcherIsServiceUnavailable(t *testing.T) {
	h := newHandler(&fakeStore{settings: &repository.ServerSettings{}}) // nil sender
	_, err := h.TestDelivery(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeServiceUnavailable)
}

func TestHandler_Deliveries_mapsRecords(t *testing.T) {
	when := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	sender := &fakeSender{recent: []svc.DeliveryRecord{
		{ID: 7, Time: when, Event: svc.EventCompleted, VideoID: 42, Outcome: svc.OutcomeDelivered, Status: 200, Attempts: 1, MessageID: "msg"},
	}}
	h := newHandlerWithSender(&fakeStore{settings: &repository.ServerSettings{}}, sender)
	out, err := h.Deliveries(context.Background())
	if err != nil {
		t.Fatalf("Deliveries: %v", err)
	}
	if len(out) != 1 || out[0].VideoID != 42 || out[0].Outcome != "delivered" || out[0].Status != 200 {
		t.Fatalf("unexpected deliveries: %+v", out)
	}
	if out[0].ID != 7 || out[0].MessageID != "msg" {
		t.Fatalf("id/message_id not mapped: %+v", out[0])
	}
	if out[0].Time != when.Format(time.RFC3339Nano) {
		t.Fatalf("time = %q, want RFC3339Nano", out[0].Time)
	}
}

func TestHandler_Deliveries_internalErrorMapsToInternal(t *testing.T) {
	sender := &fakeSender{recentErr: errHandlerBoom}
	h := newHandlerWithSender(&fakeStore{settings: &repository.ServerSettings{}}, sender)
	_, err := h.Deliveries(context.Background())
	requireTRPCCode(t, err, trpcgo.CodeInternalServerError)
}

func TestHandler_RetryDelivery_mapsRecord(t *testing.T) {
	when := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	sender := &fakeSender{retried: svc.DeliveryRecord{
		ID: 9, Time: when, Event: svc.EventFailed, VideoID: 77, Outcome: svc.OutcomePending, MessageID: "retry-msg",
	}}
	h := newHandlerWithSender(&fakeStore{settings: &repository.ServerSettings{}}, sender)
	out, err := h.RetryDelivery(context.Background(), RecordingWebhookRetryDeliveryInput{ID: 9})
	if err != nil {
		t.Fatalf("RetryDelivery: %v", err)
	}
	if out.ID != 9 || out.Outcome != "pending" || out.MessageID != "retry-msg" {
		t.Fatalf("unexpected retry response: %+v", out)
	}
}

func TestHandler_RetryDelivery_notRetryableMapsToNotFound(t *testing.T) {
	// A delivery that is missing or not in a retryable state must surface as a
	// 404, not a 500 — and never as a success that implies a re-send happened.
	sender := &fakeSender{retryErr: svc.ErrDeliveryNotRetryable}
	h := newHandlerWithSender(&fakeStore{settings: &repository.ServerSettings{}}, sender)
	_, err := h.RetryDelivery(context.Background(), RecordingWebhookRetryDeliveryInput{ID: 123})
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
}

func TestHandler_RetryDelivery_internalErrorMapsToInternal(t *testing.T) {
	sender := &fakeSender{retryErr: errHandlerBoom}
	h := newHandlerWithSender(&fakeStore{settings: &repository.ServerSettings{}}, sender)
	_, err := h.RetryDelivery(context.Background(), RecordingWebhookRetryDeliveryInput{ID: 123})
	requireTRPCCode(t, err, trpcgo.CodeInternalServerError)
}

func TestHandler_RetryDelivery_noDispatcherIsServiceUnavailable(t *testing.T) {
	h := newHandler(&fakeStore{settings: &repository.ServerSettings{}})
	_, err := h.RetryDelivery(context.Background(), RecordingWebhookRetryDeliveryInput{ID: 123})
	requireTRPCCode(t, err, trpcgo.CodeServiceUnavailable)
}

func TestHandler_Deliveries_noDispatcherReturnsEmpty(t *testing.T) {
	h := newHandler(&fakeStore{settings: &repository.ServerSettings{}}) // nil sender
	out, err := h.Deliveries(context.Background())
	if err != nil || len(out) != 0 {
		t.Fatalf("want empty no-error, got out=%v err=%v", out, err)
	}
}
