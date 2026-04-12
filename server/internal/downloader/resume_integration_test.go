package downloader

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// newTestService spins up a Service backed by a fresh SQLite repo
// and a tempdir scratch. Skips the service-account refresher wire-
// up — resume paths that hit the refresher aren't exercised here.
func newTestService(t *testing.T, scratchDir string) *Service {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(sqlitegen.New(db))
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
	return NewService(cfg, repo, store, discardLog())
}

func TestResume_EmptyDBSweepsOrphans(t *testing.T) {
	scratch := t.TempDir()
	// Seed orphan dirs under scratch — no RUNNING jobs reference
	// them, so Resume should wipe all three.
	for _, name := range []string{"orphan-a", "orphan-b", "orphan-c"} {
		if err := os.Mkdir(filepath.Join(scratch, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	s := newTestService(t, scratch)

	if err := s.Resume(context.Background()); err != nil {
		t.Fatalf("Resume on empty DB: %v", err)
	}

	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("scratch not swept: got %v, want empty", names)
	}
}

func TestSweepOrphanedTempsExcept_PreservesProtected(t *testing.T) {
	scratch := t.TempDir()
	// Three job dirs — two "protected" (RUNNING), one orphan.
	for _, name := range []string{"job-alpha", "job-beta", "job-orphan"} {
		if err := os.Mkdir(filepath.Join(scratch, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	s := newTestService(t, scratch)
	s.sweepOrphanedTempsExcept(map[string]bool{
		"job-alpha": true,
		"job-beta":  true,
	})

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
		t.Errorf("scratch after protected sweep = %v, want %v", got, want)
	}
}

func TestSweepOrphanedTempsExcept_NilProtectedWipesAll(t *testing.T) {
	scratch := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(scratch, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	s := newTestService(t, scratch)
	s.sweepOrphanedTempsExcept(nil)
	entries, _ := os.ReadDir(scratch)
	if len(entries) != 0 {
		t.Errorf("nil-protected sweep left %d entries, want 0", len(entries))
	}
}

func TestSweepOrphanedTempsExcept_MissingScratchDirIsNoop(t *testing.T) {
	scratch := filepath.Join(t.TempDir(), "does-not-exist")
	s := newTestService(t, scratch)
	// Must not panic or error — startup on a fresh deploy may hit
	// this before the operator has created the scratch tree.
	s.sweepOrphanedTempsExcept(nil)
}
