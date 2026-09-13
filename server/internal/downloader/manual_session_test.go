package downloader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	provider "github.com/befabri/replayvod/server/internal/twitch"
)

func TestManualChildReleasesResourcesBeforeCompletionNotification(t *testing.T) {
	for _, failure := range []string{"deferred claim", "panic"} {
		t.Run(failure, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			d := seedWebhookAttempt(t, s, "manual-child-resources")
			d.recovered = true
			d.progressCh = make(chan Progress)
			workspace, err := s.storage.Scratch().Open(filepath.Join(s.cfg.Env.ScratchDir, d.jobID), 0)
			if err != nil {
				t.Fatal(err)
			}
			d.workspace = workspace
			s.observe = func(context.Context, string) (*provider.Stream, error) {
				if failure == "panic" {
					panic("identity lookup failed")
				}
				return nil, errors.New("identity temporarily unavailable")
			}

			// Block completion delivery so cleanup cannot depend on the intent
			// event loop draining its inbox after a persistence outage.
			m := &manualRun{id: "manual", events: make(chan manualEvent), closed: make(chan struct{})}
			session := &manualSession{
				service: s, run: m, children: background.NewScope(t.Context()),
				finished: make(map[string]bool), launched: make(map[string]bool),
			}
			t.Cleanup(func() {
				close(m.closed)
				_ = session.children.Join()
				s.Shutdown()
			})
			streamID := "original"
			if err := session.launch(d, Params{BroadcasterID: "webhook", StreamID: &streamID}, d.jobID); err != nil {
				t.Fatal(err)
			}
			select {
			case _, open := <-d.progressCh:
				if open {
					t.Fatal("failed identity check started capture")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("blocked completion notification retained the progress channel")
			}
			ownership, err := s.storage.Scratch().OpenForCleanup(workspace.Dir)
			if err != nil {
				t.Fatalf("closed progress channel retained scratch ownership: %v", err)
			}
			if err := ownership.Close(false); err != nil {
				t.Fatal(err)
			}
			select {
			case event := <-m.events:
				if !event.done || event.jobID != d.jobID {
					t.Fatalf("unexpected completion: %+v", event)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("child did not report completion after releasing its resources")
			}
		})
	}
}
