package mediastore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/storage"
)

func diskUsage(dir string) (int64, error) {
	usage, err := scanDiskUsage(dir)
	return usage.bytes, err
}

func filesystemUsage(root fs.FS) (int64, error) {
	usage, err := scanFilesystemUsage(root, ".")
	return usage.bytes, err
}

type changingScratchFS struct {
	fs.FS
	beforeInfo func(string) error
	reads      int
}

func (f *changingScratchFS) ReadDir(name string) ([]fs.DirEntry, error) {
	f.reads++
	entries, err := fs.ReadDir(f.FS, name)
	for i, entry := range entries {
		entries[i] = changingScratchEntry{DirEntry: entry, beforeInfo: f.beforeInfo}
	}
	return entries, err
}

type changingScratchEntry struct {
	fs.DirEntry
	beforeInfo func(string) error
}

func (e changingScratchEntry) Info() (fs.FileInfo, error) {
	if err := e.beforeInfo(e.Name()); err != nil {
		return nil, err
	}
	return e.DirEntry.Info()
}

func TestScratchScanRetriesWholeSnapshotAfterRename(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a-existing.ts", "segment.ts.part"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("segment"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scan := &changingScratchFS{FS: os.DirFS(dir), beforeInfo: func(name string) error {
		if name == "segment.ts.part" {
			return os.Rename(filepath.Join(dir, name), filepath.Join(dir, "segment.ts"))
		}
		return nil
	}}
	used, err := filesystemUsage(scan)
	if err != nil || used != 2*int64(len("segment")) || scan.reads != 2 {
		t.Fatalf("rename scan = %d bytes, %d listings, %v; want both files counted once after retry", used, scan.reads, err)
	}
}

func TestScratchScanBoundsChurnAndPreservesRealErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "segment.ts.part"), []byte("segment"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{fs.ErrNotExist, fs.ErrPermission} {
		t.Run(cause.Error(), func(t *testing.T) {
			scan := &changingScratchFS{FS: os.DirFS(dir), beforeInfo: func(string) error { return cause }}
			used, err := filesystemUsage(scan)
			if errors.Is(cause, fs.ErrNotExist) {
				if !errors.Is(err, errScratchScanChanged) || used != 0 || scan.reads < 2 || scan.reads > scratchScanAttempts {
					t.Fatalf("continuous churn published usage or escaped retry bound: used=%d reads=%d err=%v", used, scan.reads, err)
				}
			} else if !errors.Is(err, cause) || used != 0 || scan.reads != 1 {
				t.Fatalf("real filesystem failure was hidden or retried: used=%d reads=%d err=%v", used, scan.reads, err)
			}
		})
	}
}

func TestScratchDeletionRestoresUnwrittenReservation(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	scratch.stat = func(string) (int64, int64, error) {
		used, err := diskUsage(scratch.Root())
		return 1_000_000, 600_000 - used, err
	}
	writer, err := scratch.New("recording", 400_000)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(true)
	saved := filepath.Join(writer.Dir, "segment.ts")
	if err := os.WriteFile(saved, make([]byte, 400_000), 0600); err != nil {
		t.Fatal(err)
	}
	peer, err := scratch.New("peer", 140_000)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(true)
	if err := os.Remove(saved); err != nil {
		t.Fatal(err)
	}
	// Deleted bytes remain promised to the first writer's future footprint.
	if err := peer.Reserve(160_000); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("removed bytes still credited as written: %v", err)
	}
	if err := writer.Close(true); err != nil {
		t.Fatal(err)
	}
	if err := peer.Reserve(160_000); err != nil {
		t.Fatalf("closed recording kept its reservation: %v", err)
	}
}

func TestScratchAccountingSurvivesConcurrentSegmentCommits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reserved int64
		size     int
	}{
		{name: "unreserved capture", size: 7},
		{name: "written reservation near capacity", reserved: 400_000, size: 400_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testScratchSegmentRenames(t, tc.reserved, tc.size)
		})
	}
}

func testScratchSegmentRenames(t *testing.T, reserved int64, size int) {
	t.Helper()
	scratch := NewScratch(t.TempDir())
	available := int64(600_000)
	scratch.stat = func(string) (int64, int64, error) { return 1_000_000, available, nil }
	writer, err := scratch.New("capture", reserved)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(true)
	peer, err := scratch.New("another-recording", 16)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(true)
	partial, committed := filepath.Join(writer.Dir, "segment.ts.part"), filepath.Join(writer.Dir, "segment.ts")
	if err := os.WriteFile(partial, make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
	// Statfs already charges these bytes; renaming must not charge them again
	// as future reservation growth.
	available -= int64(size)
	if err := peer.Reserve(16); err != nil {
		t.Fatalf("stable segment did not fit before rename: %v", err)
	}
	stop, started, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Errorf("rename segment: %v", err)
		}
	}()
	go func() {
		close(started)
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			for _, paths := range [][2]string{{partial, committed}, {committed, partial}} {
				if err := writer.Rename(paths[0], paths[1]); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	<-started
	for range 2000 {
		if err := peer.Reserve(16); err != nil {
			t.Fatalf("segment rename changed accounting with %d bytes available: %v", available, err)
		}
	}
}

func TestScratchCopyAccountsUnknownSizeAndCleansPartialOutput(t *testing.T) {
	repo, raw, _ := mediaFixture(t)
	store := managed(t, repo, raw)
	scratch := store.Scratch()
	scratch.stat = func(string) (int64, int64, error) {
		used, err := diskUsage(scratch.Root())
		return 1_000_000, 220_000 - used, err
	}
	if err := raw.Save(t.Context(), "videos/input", strings.NewReader(strings.Repeat("m", 128_000))); err != nil {
		t.Fatal(err)
	}
	w, err := scratch.New("copy", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(true)
	path, err := w.Copy(t.Context(), store, "videos/input", "input")
	if err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(path); err != nil || len(body) != 128_000 {
		t.Fatalf("copy = %d bytes, %v", len(body), err)
	}
	// Existing input plus the future output must share the same reservation.
	if err := w.ReserveAdditional(t.Context(), 30_000); err != nil {
		t.Fatal(err)
	}
	if _, err := scratch.New("competing", 20_000); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("additional output not reserved: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other, err := scratch.New("other", 10_000)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(true)
	scratch.stat = func(string) (int64, int64, error) {
		used, err := diskUsage(scratch.Root())
		return 1_000_000, 120_000 - used, err
	}
	ctx, stop := w.Monitor(context.Background())
	defer stop()
	if _, err := w.Copy(ctx, store, "videos/input", "partial"); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("copy exceeded budget: %v", err)
	}
	if !errors.Is(context.Cause(ctx), storage.ErrFull) {
		t.Fatalf("writer did not cancel owner: %v", context.Cause(ctx))
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "partial")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial output remained: %v", err)
	}
}

func TestScratchCopyReplacementRestoresUnwrittenReservation(t *testing.T) {
	repo, raw, _ := mediaFixture(t)
	store := managed(t, repo, raw)
	scratch := store.Scratch()
	scratch.stat = func(string) (int64, int64, error) {
		used, err := diskUsage(scratch.Root())
		return 1_000_000, 600_000 - used, err
	}
	writer, err := scratch.New("copy", 400_000)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(true)
	if err := os.WriteFile(filepath.Join(writer.Dir, "input"), make([]byte, 400_000), 0600); err != nil {
		t.Fatal(err)
	}
	peer, err := scratch.New("peer", 140_000)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(true)
	if err := raw.Save(t.Context(), "videos/input", strings.NewReader(strings.Repeat("m", 10_000))); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Copy(t.Context(), store, "videos/input", "input"); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(peer.Dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if n, err := peer.WriteFile(t.Context(), file, make([]byte, 160_000)); n != 0 || !errors.Is(err, storage.ErrFull) {
		t.Fatalf("replacement released another writer's reservation: wrote=%d err=%v", n, err)
	}
}

func TestScratchCleanupOwnsDirectoryWithoutCapacity(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	w, err := scratch.New("recording", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(false)
	saved := filepath.Join(w.Dir, "saved.ts")
	if err := os.WriteFile(saved, []byte("saved media"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{w.Dir, filepath.Join(w.Dir, "part01")} {
		if _, err := scratch.OpenForCleanup(dir); !errors.Is(err, ErrWorkspaceBusy) {
			t.Fatalf("cleanup acquired active capture %s: %v", dir, err)
		}
	}
	if err := w.Close(false); err != nil {
		t.Fatal(err)
	}
	scratch.stat = func(string) (int64, int64, error) { return 1_000_000, 0, nil }
	if _, err := scratch.Open(w.Dir, 0); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("capture did not encounter full scratch: %v", err)
	}
	cleanup, err := scratch.OpenForCleanup(w.Dir)
	if err != nil {
		t.Fatalf("full scratch prevented cleanup ownership: %v", err)
	}
	defer cleanup.Close(false)
	if err := w.Close(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("stale workspace removed another owner's files: %v", err)
	}
	if _, err := scratch.Open(w.Dir, 0); !errors.Is(err, ErrWorkspaceBusy) {
		t.Fatalf("capture raced cleanup: %v", err)
	}
	if err := cleanup.Close(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); !errors.Is(err, os.ErrNotExist) || len(scratch.work) != 0 {
		t.Fatalf("cleanup leaked directory or reservation: %v, work=%d", err, len(scratch.work))
	}
	for _, dir := range []string{scratch.Root(), filepath.Dir(scratch.Root())} {
		if _, err := scratch.OpenForCleanup(dir); err == nil {
			t.Fatalf("cleanup accepted scratch root or parent: %s", dir)
		}
	}
}
