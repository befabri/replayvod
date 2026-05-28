package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/service/eventsubconfig"
)

// TestAwaitLivePollShutdown pins the shutdown grace: a nil channel returns at
// once (poller never started), a closed channel returns immediately (poller
// stopped cleanly), and an open channel returns after the grace with a warning
// (poller stuck) rather than blocking forever.
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

// TestResolveOrDegrade pins the boot policy for eventsubconfig.Resolve's
// outcomes: a clean resolve is used as-is; an invalid app-managed config degrades
// to setup-required (so the owner can re-onboard rather than the process refusing
// to boot); an invalid env-managed config or any non-ErrInvalid error is fatal.
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

// TestValidateRelayURLs_AcceptsAlignedConfig pins the relay URL invariant:
// when the subscribe and ingest URLs are usable HTTPS/WSS endpoints with the
// same relay token, validation must succeed.
func TestValidateRelayURLs_AcceptsAlignedConfig(t *testing.T) {
	err := config.ValidateRelayURLs(
		"https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err != nil {
		t.Fatalf("validateRelayURLs(aligned) = %v, want nil", err)
	}
}

// TestValidateRelayURLs_RejectsPlaintextIngest pins the behavior that a
// non-HTTPS ingest URL fails when a subscribe URL is set. Relay mode always
// requires public HTTPS/WSS.
func TestValidateRelayURLs_RejectsPlaintextIngest(t *testing.T) {
	err := config.ValidateRelayURLs(
		"http://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err == nil {
		t.Fatal("validateRelayURLs(plaintext ingest) = nil, want error")
	}
}

// TestValidateRelayURLs_RelayDisabled pins the behavior that an empty
// subscribe URL disables the pair check entirely.
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
