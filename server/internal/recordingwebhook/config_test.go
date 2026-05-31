package recordingwebhook

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestServiceGet_missingRowIsDisabled(t *testing.T) {
	svc := New(&fakeRepo{settingsErr: repository.ErrNotFound}, nil)
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg.Enabled || cfg.URL != "" || cfg.Secret != "" || cfg.Events != nil {
		t.Fatalf("missing row should yield zero config, got %+v", cfg)
	}
}

func TestServiceGet_parsesEvents(t *testing.T) {
	svc := New(&fakeRepo{settings: &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     "https://hooks.example/x",
		RecordingWebhookSecret:  "sek",
		RecordingWebhookEvents:  "recording.failed",
	}}, nil)
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !cfg.Enabled || cfg.URL != "https://hooks.example/x" || cfg.Secret != "sek" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.Events) != 1 || cfg.Events[0] != EventFailed {
		t.Fatalf("events = %v, want [recording.failed]", cfg.Events)
	}
}

func TestServiceUpdate_autoGeneratesSecretWhenEnabledAndBlank(t *testing.T) {
	repo := &fakeRepo{settings: &repository.ServerSettings{}} // no secret yet
	svc := New(repo, nil)

	cfg, err := svc.Update(context.Background(), UpdateInput{
		Enabled: true,
		URL:     "https://hooks.example/recordings",
		Events:  []string{EventCompleted},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(cfg.Secret) != 64 {
		t.Fatalf("expected a generated 64-char secret, got %q (len %d)", cfg.Secret, len(cfg.Secret))
	}
	// Enabling seeds the secret via the CAS ensure, never the unconditional set.
	if repo.ensureCalls != 1 || repo.setCalls != 0 {
		t.Fatalf("expected one ensure and no set, got ensure=%d set=%d", repo.ensureCalls, repo.setCalls)
	}
	if repo.settings.RecordingWebhookSecret != cfg.Secret {
		t.Fatalf("persisted secret %q != returned %q", repo.settings.RecordingWebhookSecret, cfg.Secret)
	}
}

func TestServiceUpdate_preservesSecretOnEdit(t *testing.T) {
	repo := &fakeRepo{settings: &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     "https://old.example/x",
		RecordingWebhookSecret:  "keep-me",
	}}
	svc := New(repo, nil)

	cfg, err := svc.Update(context.Background(), UpdateInput{
		Enabled: true,
		URL:     "https://new.example/y",
		Events:  []string{EventCompleted, EventFailed},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cfg.Secret != "keep-me" {
		t.Fatalf("secret = %q, want preserved keep-me", cfg.Secret)
	}
	if cfg.URL != "https://new.example/y" {
		t.Fatalf("url = %q, want updated", cfg.URL)
	}
}

func TestServiceRegenerateSecret_rotatesUnconditionally(t *testing.T) {
	repo := &fakeRepo{settings: &repository.ServerSettings{
		RecordingWebhookEnabled: true,
		RecordingWebhookURL:     "https://hooks.example/x",
		RecordingWebhookSecret:  "old-secret",
	}}
	svc := New(repo, nil)

	cfg, err := svc.RegenerateSecret(context.Background())
	if err != nil {
		t.Fatalf("RegenerateSecret: %v", err)
	}
	if cfg.Secret == "old-secret" || len(cfg.Secret) != 64 {
		t.Fatalf("secret = %q, want a fresh 64-char value", cfg.Secret)
	}
	// Rotation is the unconditional set, and never touches the config columns.
	if repo.setCalls != 1 || len(repo.upsertCalls) != 0 {
		t.Fatalf("expected one set and no config upsert, got set=%d upserts=%d", repo.setCalls, len(repo.upsertCalls))
	}
	if cfg.URL != "https://hooks.example/x" || !cfg.Enabled {
		t.Fatalf("regenerate must not disturb config, got %+v", cfg)
	}
}

func TestServiceUpdate_rejectsBadURLWhenEnabled(t *testing.T) {
	svc := New(&fakeRepo{settings: &repository.ServerSettings{}}, nil)
	_, err := svc.Update(context.Background(), UpdateInput{Enabled: true, URL: "http://public.example/x"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Update with bad URL err = %v, want ErrInvalid", err)
	}
}

func TestServiceUpdate_rejectsBadURLEvenWhenDisabled(t *testing.T) {
	// A non-empty URL is validated regardless of the enabled flag, so a disabled
	// config can never silently store junk that surprises the owner on enable.
	svc := New(&fakeRepo{settings: &repository.ServerSettings{}}, nil)
	_, err := svc.Update(context.Background(), UpdateInput{Enabled: false, URL: "ftp://nope.example/x"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("disabled+bad URL err = %v, want ErrInvalid", err)
	}
}

func TestServiceUpdate_requiresURLWhenEnabled(t *testing.T) {
	svc := New(&fakeRepo{settings: &repository.ServerSettings{}}, nil)
	_, err := svc.Update(context.Background(), UpdateInput{Enabled: true, URL: "  "})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Update without URL err = %v, want ErrInvalid", err)
	}
}

func TestServiceUpdate_rejectsEnabledWithNoEvents(t *testing.T) {
	// The server enforces the "pick at least one event" invariant itself, not
	// just the dashboard: an enabled webhook with [] events must be rejected, not
	// silently treated as "all".
	svc := New(&fakeRepo{settings: &repository.ServerSettings{}}, nil)
	_, err := svc.Update(context.Background(), UpdateInput{
		Enabled: true,
		URL:     "https://hooks.example/x",
		Events:  nil,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("enabled+no-events err = %v, want ErrInvalid", err)
	}
}

func TestServiceUpdate_disabledMayHaveNoEvents(t *testing.T) {
	// The invariant only applies when enabled; disabling with no events is fine.
	repo := &fakeRepo{settings: &repository.ServerSettings{}}
	if _, err := New(repo, nil).Update(context.Background(), UpdateInput{Enabled: false, Events: nil}); err != nil {
		t.Fatalf("disabled + no events should be allowed, got %v", err)
	}
}

func TestServiceUpdate_disabledKeepsConfigButDoesNotRequireURL(t *testing.T) {
	repo := &fakeRepo{settings: &repository.ServerSettings{}}
	svc := New(repo, nil)
	cfg, err := svc.Update(context.Background(), UpdateInput{Enabled: false, URL: ""})
	if err != nil {
		t.Fatalf("disabling should not error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("config should be disabled")
	}
	// A disabled webhook with no prior secret should not invent one.
	if cfg.Secret != "" {
		t.Fatalf("secret = %q, want empty for disabled+no-prior-secret", cfg.Secret)
	}
}

func TestServiceUpdate_normalizesAndPersistsEvents(t *testing.T) {
	repo := &fakeRepo{settings: &repository.ServerSettings{RecordingWebhookSecret: "s"}}
	svc := New(repo, nil)
	_, err := svc.Update(context.Background(), UpdateInput{
		Enabled: true,
		URL:     "https://hooks.example/x",
		Events:  []string{"recording.failed", "recording.completed", "recording.failed"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	last, _ := repo.lastUpsert()
	if last.events != "recording.completed,recording.failed" {
		t.Fatalf("persisted events = %q, want canonical comma list", last.events)
	}
}

func TestServiceUpdate_rejectsUnknownEvent(t *testing.T) {
	svc := New(&fakeRepo{settings: &repository.ServerSettings{}}, nil)
	_, err := svc.Update(context.Background(), UpdateInput{
		Enabled: true,
		URL:     "https://hooks.example/x",
		Events:  []string{"recording.exploded"},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown event err = %v, want ErrInvalid", err)
	}
}
