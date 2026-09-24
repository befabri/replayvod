package downloader

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func newTestService(t *testing.T, scratchDir string) *Service {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{
			ScratchDir: scratchDir,
		},
		App: config.AppConfig{
			Download: config.DownloadConfig{
				MaxConcurrent:      2,
				SegmentConcurrency: 4,
			},
		},
	}
	return NewService(cfg, repo, mediatest.NewAt(t, repo, store, nil, nil, cfg.Env.ScratchDir), nil, nil, nil, discardLog())
}

func TestPrepareScratch_SweepsOrphansButKeepsRecoverableJobs(t *testing.T) {
	scratch := t.TempDir()
	for _, name := range []string{"job-alpha", "job-beta", "orphan-a", "orphan-b"} {
		if err := os.Mkdir(filepath.Join(scratch, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	s := newTestService(t, scratch)
	seedWebhookAttempt(t, s, "job-alpha")
	seedWebhookAttempt(t, s, "job-beta")

	if err := s.PrepareScratch(context.Background()); err != nil {
		t.Fatalf("PrepareScratch: %v", err)
	}

	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Name()
	}
	slices.Sort(got)
	want := []string{"job-alpha", "job-beta"}
	if !slices.Equal(got, want) {
		t.Errorf("scratch after startup sweep = %v, want %v", got, want)
	}
}

func TestPrepareScratch_MissingScratchDirIsNoop(t *testing.T) {
	scratch := filepath.Join(t.TempDir(), "does-not-exist")
	s := newTestService(t, scratch)
	// Startup on a fresh deploy may run before the operator has
	// created the scratch tree; that must not fail bootstrap.
	if err := s.PrepareScratch(context.Background()); err != nil {
		t.Fatalf("PrepareScratch without a scratch dir: %v", err)
	}
}
