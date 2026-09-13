package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/videodownload"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/service/eventsubconfig"
)

func TestAwaitLivePollShutdown(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	awaitLivePollShutdown(nil, time.Second, quiet) // must not block

	closed := make(chan struct{})
	close(closed)
	awaitLivePollShutdown(closed, time.Second, quiet) // returns via <-done

	var buf bytes.Buffer
	warnLog := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	open := make(chan struct{}) // never closed -> grace elapses
	awaitLivePollShutdown(open, 10*time.Millisecond, warnLog)
	if !bytes.Contains(buf.Bytes(), []byte("did not stop within shutdown grace")) {
		t.Fatalf("expected a grace-period warning; got:\n%s", buf.String())
	}
}

func TestRecordingWebhookDownloadURLsMatchRetentionAvailability(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		interval int
		wantURL  bool
	}{
		{"retention available", true, 60, false},
		{"scheduler disabled", false, 60, true},
		{"retention disabled", true, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db := testdb.NewSQLiteDB(t)
			repo := sqliteadapter.New(db)
			if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "recording", BroadcasterLogin: "recording", BroadcasterName: "Recording"}); err != nil {
				t.Fatal(err)
			}
			hours := int64(1)
			v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "recording", Filename: "recording", BroadcasterID: "recording", Status: repository.VideoStatusDone, RetentionWindowHours: &hours})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: v.ID, PartIndex: 1, Filename: "recording.mp4", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "UPDATE videos SET downloaded_at = datetime('now', '-2 hours') WHERE id = ?", v.ID); err != nil {
				t.Fatal(err)
			}
			received := make(chan recordingwebhook.Payload, 1)
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload recordingwebhook.Payload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				received <- payload
				w.WriteHeader(http.StatusNoContent)
			}))
			defer receiver.Close()
			if _, err := repo.UpsertRecordingWebhookConfig(ctx, true, receiver.URL, recordingwebhook.EventCompleted); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetRecordingWebhookSecret(ctx, "secret"); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CreateRecordingWebhookDelivery(ctx, recordingwebhook.NewTerminalDeliveryInput(recordingwebhook.EventCompleted, v.ID, time.Now())); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: tc.enabled, RecordingsRetentionIntervalMinutes: tc.interval}}}
			signer := videodownload.NewSigner("secret", "https://app.example", time.Hour)
			dispatcher := newRecordingWebhookDispatcher(cfg, repo, signer, slog.New(slog.DiscardHandler))
			deliveryCtx, cancel := context.WithCancel(ctx)
			dispatcher.Start(deliveryCtx, nil)
			defer func() { cancel(); dispatcher.Wait() }()
			select {
			case payload := <-received:
				if len(payload.Parts) != 1 || (payload.Parts[0].DownloadURL != "") != tc.wantURL {
					t.Fatalf("download URL does not match retention availability: %+v", payload.Parts)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("recording webhook was not delivered")
			}
		})
	}
}

func TestResolveOrDegrade(t *testing.T) {
	envInvalid := config.ServerModeConfig{Source: config.ServerModeConfigSourceEnv, Mode: config.ServerModeDirect}
	appInvalid := config.ServerModeConfig{Source: config.ServerModeConfigSourceApp, Mode: config.ServerModeDirect}
	valid := config.ServerModeConfig{Source: config.ServerModeConfigSourceEnv, Mode: config.ServerModeOff}

	tests := []struct {
		name       string
		resolved   config.ServerModeConfig
		err        error
		wantMode   string
		wantSource string
		wantFatal  bool
	}{
		{
			name:       "clean resolve is used as-is",
			resolved:   valid,
			err:        nil,
			wantMode:   config.ServerModeOff,
			wantSource: config.ServerModeConfigSourceEnv,
			wantFatal:  false,
		},
		{
			name:       "invalid app config degrades to setup required",
			resolved:   appInvalid,
			err:        fmt.Errorf("bad callback: %w", eventsubconfig.ErrInvalid),
			wantMode:   "",
			wantSource: config.ServerModeConfigSourceUnset,
			wantFatal:  false,
		},
		{
			name:      "invalid env config is fatal",
			resolved:  envInvalid,
			err:       fmt.Errorf("bad callback: %w", eventsubconfig.ErrInvalid),
			wantFatal: true,
		},
		{
			name:      "non-invalid error (e.g. DB read failure) is fatal",
			resolved:  appInvalid,
			err:       errors.New("load server settings: connection refused"),
			wantFatal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			final, fatal := resolveOrDegrade(tt.resolved, tt.err)
			if fatal != tt.wantFatal {
				t.Fatalf("fatal = %v, want %v", fatal, tt.wantFatal)
			}
			if tt.wantFatal {
				return
			}
			if final.Mode != tt.wantMode {
				t.Fatalf("final.Mode = %q, want %q", final.Mode, tt.wantMode)
			}
			if final.Source != tt.wantSource {
				t.Fatalf("final.Source = %q, want %q", final.Source, tt.wantSource)
			}
		})
	}
}

func TestValidateRelayURLs_RejectsTokenMismatch(t *testing.T) {
	const (
		ingestURL    = "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"
		subscribeURL = "wss://relay.replayvod.com/u/BBBBBBBBBBBBBBBB/subscribe"
	)

	err := config.ValidateRelayURLs(ingestURL, subscribeURL)
	if err == nil {
		t.Fatal("validateRelayURLs(token mismatch) = nil, want error")
	}
}

func TestValidateRelayURLs_RejectsHostMismatch(t *testing.T) {
	err := config.ValidateRelayURLs(
		"https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://other.example/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err == nil {
		t.Fatal("validateRelayURLs(host mismatch) = nil, want error")
	}
}

func TestValidateRelayURLs_RejectsPlaintextSubscribeURL(t *testing.T) {
	err := config.ValidateRelayURLs(
		"https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"ws://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err == nil {
		t.Fatal("validateRelayURLs(ws subscribe) = nil, want error")
	}
}

func TestValidateRelayURLs_AcceptsAlignedConfig(t *testing.T) {
	err := config.ValidateRelayURLs(
		"https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err != nil {
		t.Fatalf("validateRelayURLs(aligned) = %v, want nil", err)
	}
}

func TestValidateRelayURLs_RejectsPlaintextIngest(t *testing.T) {
	err := config.ValidateRelayURLs(
		"http://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err == nil {
		t.Fatal("validateRelayURLs(plaintext ingest) = nil, want error")
	}
}

func TestValidateRelayURLs_RelayDisabled(t *testing.T) {
	err := config.ValidateRelayURLs("", "")
	if err != nil {
		t.Fatalf("validateRelayURLs(empty) = %v, want nil", err)
	}
}

func TestValidateServerMode_AcceptsRelay(t *testing.T) {
	cfg := config.ServerModeConfig{
		Mode:              config.ServerModeRelay,
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	}

	if err := config.ValidateServerMode(cfg); err != nil {
		t.Fatalf("validateServerMode(relay) = %v, want nil", err)
	}
}

func TestValidateServerMode_RejectsImplicitRelay(t *testing.T) {
	cfg := config.ServerModeConfig{
		Mode:              config.ServerModeOff,
		RelayIngestURL:    "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		RelaySubscribeURL: "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	}

	if err := config.ValidateServerMode(cfg); err == nil {
		t.Fatal("validateServerMode(off with relay URLs) = nil, want error")
	}
}

func TestValidateServerMode_RejectsDirectWithRelayURLs(t *testing.T) {
	cfg := config.ServerModeConfig{
		Mode:               config.ServerModeDirect,
		WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback",
		RelayIngestURL:     "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
	}

	if err := config.ValidateServerMode(cfg); err == nil {
		t.Fatal("validateServerMode(direct with relay URL) = nil, want error")
	}
}

func TestValidateServerMode_RejectsLocalDirectCallback(t *testing.T) {
	cfg := config.ServerModeConfig{
		Mode:               config.ServerModeDirect,
		WebhookCallbackURL: "https://localhost/api/v1/webhook/callback",
	}

	if err := config.ValidateServerMode(cfg); err == nil {
		t.Fatal("validateServerMode(direct with localhost callback) = nil, want error")
	}
}

func TestValidateServerMode_AcceptsPoll(t *testing.T) {
	cfg := config.ServerModeConfig{Mode: config.ServerModePoll}
	if err := config.ValidateServerMode(cfg); err != nil {
		t.Fatalf("validateServerMode(poll) = %v, want nil", err)
	}
}

type bootStorageMonitor struct {
	stopped     chan struct{}
	attachCalls int
	onAttach    func()
}

func (m *bootStorageMonitor) Attach(context.Context) (storagehealth.Status, error) {
	m.attachCalls++
	if m.onAttach != nil {
		m.onAttach()
	}
	return storagehealth.Status{State: storagehealth.StateUnattached, Reason: "marker missing"}, storage.ErrUnattached
}
func (m *bootStorageMonitor) Verify(context.Context) error { return storage.ErrUnattached }
func (m *bootStorageMonitor) Ready() error                 { return storage.ErrUnattached }
func (m *bootStorageMonitor) Run(ctx context.Context)      { <-ctx.Done(); close(m.stopped) }

type bootStorageDownloads struct {
	resumed chan struct{}
}

func (d *bootStorageDownloads) Resume(context.Context) error {
	d.resumed <- struct{}{}
	return errors.New("injected resume failure")
}
func TestAttachStorageResumesOnlyOnWritableRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	mon := &bootStorageMonitor{stopped: make(chan struct{})}
	dl := &bootStorageDownloads{resumed: make(chan struct{}, 4)}
	bus := eventbus.New()
	attachStorage(ctx, mon, bus, dl, slog.New(slog.DiscardHandler))
	if mon.attachCalls != 1 {
		t.Fatalf("boot did not attach storage exactly once: %d", mon.attachCalls)
	}
	for _, state := range []string{"unattached", "read_only", "full", "unreachable", "attached", "attached"} {
		bus.StorageStatus.Publish(eventbus.StorageStatusEvent{State: state})
	}
	// The second recovery still runs after the first Resume error. FIFO delivery
	// means both received calls also prove earlier non-writable events were skipped.
	for range 2 {
		select {
		case <-dl.resumed:
		case <-time.After(time.Second):
			t.Fatal("recovery not resumed")
		}
	}
	cancel()
	select {
	case <-mon.stopped:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop")
	}
	deadline := time.After(time.Second)
	for bus.StorageStatus.Count() != 0 {
		select {
		case <-deadline:
			t.Fatal("subscription did not stop")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-dl.resumed:
		t.Fatal("non-writable storage resumed downloads")
	default:
	}
}

func TestAttachStorageSubscribesBeforeInitialAttachmentEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	bus := eventbus.New()
	mon := &bootStorageMonitor{stopped: make(chan struct{}), onAttach: func() {
		bus.StorageStatus.Publish(eventbus.StorageStatusEvent{State: "attached"})
	}}
	dl := &bootStorageDownloads{resumed: make(chan struct{}, 1)}
	attachStorage(ctx, mon, bus, dl, slog.New(slog.DiscardHandler))
	if mon.attachCalls != 1 {
		t.Fatal("initial attachment was skipped")
	}
	select {
	case <-dl.resumed:
	case <-time.After(time.Second):
		t.Fatal("attachment event emitted during Attach was lost")
	}
	cancel()
	<-mon.stopped
}
