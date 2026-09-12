package downloader

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
)

type detachOnSaveStorage struct {
	*storage.LocalStorage
	detach func() error
}

func (s *detachOnSaveStorage) Save(ctx context.Context, key string, r io.Reader) error {
	if s.detach != nil {
		detach := s.detach
		s.detach = nil
		if err := detach(); err != nil {
			return err
		}
	}
	return s.LocalStorage.Save(ctx, key, r)
}

func TestUploadVerifiesIdentityBeyondCachedReadiness(t *testing.T) {
	for _, duringSave := range []bool{false, true} {
		name := "before upload"
		if duringSave {
			name = "during successful save"
		}
		t.Run(name, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			t.Cleanup(s.Shutdown)
			local := s.storage.(*storage.LocalStorage)
			store := &detachOnSaveStorage{LocalStorage: local}
			s.storage = store
			mon := storagehealth.New(s.repo, store, nil, s.log, "local", local.Root)
			if _, err := mon.Attach(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.SetStorageGate(mon)
			detached := filepath.Join(t.TempDir(), "detached")
			detach := func() error { return os.Rename(local.Root, detached) }
			if duringSave {
				store.detach = func() error {
					if err := detach(); err != nil {
						return err
					}
					// An unmount can leave an empty directory on the host volume.
					return os.Mkdir(local.Root, 0o755)
				}
			} else if err := detach(); err != nil {
				t.Fatal(err)
			}
			if err := mon.Ready(); err != nil {
				t.Fatalf("fixture must retain stale cached success: %v", err)
			}
			scratch := filepath.Join(s.cfg.Env.ScratchDir, "recording.mp4")
			const body = "complete recording from scratch"
			if err := os.WriteFile(scratch, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				done <- s.uploadFromScratch(ctx, scratch, "videos/recording.mp4")
			}()
			t.Cleanup(func() { cancel(); <-finished })
			deadline := time.After(3 * time.Second)
			for mon.Ready() == nil {
				select {
				case err := <-done:
					t.Fatalf("upload accepted stale readiness: %v", err)
				case <-deadline:
					t.Fatal("upload never verified the missing identity")
				case <-time.After(time.Millisecond):
				}
			}
			select {
			case err := <-done:
				t.Fatalf("upload completed during outage: %v", err)
			default:
			}
			if got, err := os.ReadFile(scratch); err != nil || string(got) != body {
				t.Fatalf("retry source lost: %q %v", got, err)
			}
			if !duringSave {
				if _, err := os.Stat(local.Root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("upload recreated missing mount: %v", err)
				}
			}
			if err := os.RemoveAll(local.Root); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(detached, local.Root); err != nil {
				t.Fatal(err)
			}
			// No periodic monitor or explicit Check: the waiting upload must probe.
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("upload failed to recover")
			}
			if got, err := os.ReadFile(filepath.Join(local.Root, "videos/recording.mp4")); err != nil || string(got) != body {
				t.Fatalf("trusted storage lacks complete retry: %q %v", got, err)
			}
		})
	}
}
