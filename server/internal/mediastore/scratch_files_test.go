package mediastore

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

func TestScratchRenameWaitsForAdmissionScan(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	scratch.stat = func(string) (int64, int64, error) { return 1_000_000, 600_000, nil }
	writer, err := scratch.New("capture", 400_000)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(true)
	peer, err := scratch.New("peer", 140_000)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(true)
	partial, committed := filepath.Join(writer.Dir, "segment.part"), filepath.Join(writer.Dir, "segment.ts")
	if err := os.WriteFile(partial, make([]byte, 400_000), 0600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var scanOnce, releaseOnce sync.Once
	releaseScan := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseScan()
	scratch.usage = func(dir string) (scratchUsage, error) {
		if dir != writer.Dir {
			return scanDiskUsage(dir)
		}
		return scanFilesystemUsage(&changingScratchFS{FS: os.DirFS(dir), beforeInfo: func(string) error {
			scanOnce.Do(func() {
				close(entered)
				<-release
			})
			return nil
		}}, dir)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	wait := func(ctx context.Context, signal <-chan struct{}, operation string) bool {
		select {
		case <-signal:
			return true
		case <-ctx.Done():
			t.Errorf("timed out waiting for %s", operation)
			return false
		}
	}
	admitted, renamed := make(chan struct{}), make(chan struct{})
	var admissionErr, renameErr error
	go func() {
		defer close(admitted)
		admissionErr = peer.Reserve(140_000)
	}()
	renameStarted := false
	defer func() {
		releaseScan()
		joinCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		wait(joinCtx, admitted, "admission worker shutdown")
		if renameStarted {
			wait(joinCtx, renamed, "rename worker shutdown")
		}
	}()
	if !wait(ctx, entered, "accounting scan") {
		t.FailNow()
	}
	started := make(chan struct{})
	renameStarted = true
	go func() {
		defer close(renamed)
		close(started)
		renameErr = writer.Rename(partial, committed)
	}()
	if !wait(ctx, started, "rename worker") {
		t.FailNow()
	}
	select {
	case <-renamed:
		t.Fatalf("segment changed while admission inspected its old name: %v", renameErr)
	case <-time.After(25 * time.Millisecond):
	}
	releaseScan()
	if !wait(ctx, admitted, "admission result") || !wait(ctx, renamed, "rename result") {
		t.FailNow()
	}
	if admissionErr != nil {
		t.Fatalf("admission raced a managed rename: %v", admissionErr)
	}
	if renameErr != nil {
		t.Fatal(renameErr)
	}
	if info, err := os.Stat(committed); err != nil || info.Size() != 400_000 {
		t.Fatalf("committed segment = %v, %v", info, err)
	}
}

func TestScratchMutationsRestoreReservationBeforePeerWrite(t *testing.T) {
	for _, operation := range []string{"remove", "already removed", "truncate", "rename over existing file"} {
		t.Run(operation, func(t *testing.T) {
			scratch := NewScratch(t.TempDir())
			scratch.stat = func(string) (int64, int64, error) {
				used, err := diskUsage(scratch.Root())
				return 1_000_000, 600_000 - used, err
			}
			writer, err := scratch.New("capture", 400_000)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close(true)
			path := filepath.Join(writer.Dir, "segment.ts")
			if err := os.WriteFile(path, make([]byte, 400_000), 0600); err != nil {
				t.Fatal(err)
			}
			peer, err := scratch.New("peer", 140_000)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close(true)
			switch operation {
			case "remove":
				err = writer.Remove(path)
			case "already removed":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := writer.Remove(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("missing file removal lost its filesystem error: %v", err)
				}
			case "truncate":
				var file *os.File
				file, err = os.OpenFile(path, os.O_WRONLY, 0600)
				if err == nil {
					err = writer.Truncate(file, 0)
					err = errors.Join(err, file.Close())
				}
			case "rename over existing file":
				replacement := filepath.Join(writer.Dir, "replacement")
				if err = os.WriteFile(replacement, nil, 0600); err == nil {
					err = writer.Rename(replacement, path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(peer.Dir, "output"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if n, err := peer.WriteFile(t.Context(), file, make([]byte, 160_000)); n != 0 || !errors.Is(err, storage.ErrFull) {
				t.Fatalf("mutation released another writer's reservation: wrote=%d err=%v", n, err)
			}
			if err := writer.Close(true); err != nil {
				t.Fatal(err)
			}
			if n, err := peer.WriteFile(t.Context(), file, make([]byte, 160_000)); n != 160_000 || err != nil {
				t.Fatalf("closed writer kept its reservation: wrote=%d err=%v", n, err)
			}
		})
	}
}

func TestScratchMutationAccountsForUnscannedOutput(t *testing.T) {
	for _, operation := range []string{"remove", "replace", "rename", "truncate", "append", "overwrite"} {
		t.Run(operation, func(t *testing.T) {
			scratch := NewScratch(t.TempDir())
			scratch.stat = func(string) (int64, int64, error) {
				used, err := diskUsage(scratch.Root())
				return 1_000_000, 600_000 - used, err
			}
			writer, err := scratch.New("remux", 400_000)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close(true)
			input, err := os.Create(filepath.Join(writer.Dir, "input.mp4"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.WriteFile(t.Context(), input, make([]byte, 200_000)); err != nil {
				t.Fatal(err)
			}
			if err := input.Close(); err != nil {
				t.Fatal(err)
			}
			peer, err := scratch.New("peer", 150_000)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close(true)
			partial := filepath.Join(writer.Dir, "output.part")
			if err := os.WriteFile(partial, make([]byte, 100_000), 0600); err != nil {
				t.Fatal(err)
			}
			scratch.usage = func(string) (scratchUsage, error) {
				return scratchUsage{}, errors.New("file mutation rescanned a workspace")
			}
			switch operation {
			case "remove":
				err = writer.Remove(partial)
			case "replace":
				replacement := filepath.Join(writer.Dir, "replacement.part")
				if err = os.WriteFile(replacement, make([]byte, 50_000), 0600); err == nil {
					err = writer.Rename(replacement, partial)
				}
			case "rename":
				err = writer.Rename(partial, filepath.Join(writer.Dir, "output.mp4"))
			default:
				var output *os.File
				output, err = os.OpenFile(partial, os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if operation == "truncate" {
					err = writer.Truncate(output, 50_000)
				} else {
					if operation == "append" {
						_, err = output.Seek(0, io.SeekEnd)
					}
					if err == nil {
						_, err = writer.WriteFile(t.Context(), output, make([]byte, 50_000))
					}
				}
				err = errors.Join(err, output.Close())
			}
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(peer.Dir, "capture.ts"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if n, err := peer.WriteFile(t.Context(), file, make([]byte, 150_000)); n != 150_000 || err != nil {
				t.Fatalf("unscanned output invalidated the peer's reserved write: wrote=%d err=%v", n, err)
			}
		})
	}
}

func TestScratchFailedAccountingPreservesAllPathCredits(t *testing.T) {
	for _, phase := range []string{"scan", "statfs"} {
		t.Run(phase, func(t *testing.T) {
			testScratchFailedAccounting(t, phase)
		})
	}
}

func testScratchFailedAccounting(t *testing.T, phase string) {
	t.Helper()
	scratch := NewScratch(t.TempDir())
	scratch.stat = func(string) (int64, int64, error) { return 1_000_000, 600_000, nil }
	for range 2 {
		writer, err := scratch.New("capture", 100_000)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close(true)
		if err := os.WriteFile(filepath.Join(writer.Dir, "input"), make([]byte, 50_000), 0600); err != nil {
			t.Fatal(err)
		}
		if err := writer.Reserve(100_000); err != nil {
			t.Fatal(err)
		}
	}
	before := make(map[*Workspace]scratchUsage)
	for writer, usage := range scratch.work {
		before[writer] = scratchUsage{bytes: usage.bytes, files: maps.Clone(usage.files)}
		if err := os.WriteFile(filepath.Join(writer.Dir, "external.part"), make([]byte, 10_000), 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	scratch.usage = func(dir string) (scratchUsage, error) {
		calls++
		if calls == 2 && phase == "scan" {
			return scratchUsage{}, os.ErrPermission
		}
		return scanDiskUsage(dir)
	}
	if phase == "statfs" {
		scratch.stat = func(string) (int64, int64, error) { return 0, 0, os.ErrPermission }
	}
	scratch.mu.Lock()
	err := scratch.checkLocked()
	scratch.mu.Unlock()
	if !errors.Is(err, os.ErrPermission) || calls != 2 {
		t.Fatalf("failed accounting scan = %v, calls=%d", err, calls)
	}
	if !reflect.DeepEqual(scratch.work, before) {
		t.Fatal("failed scan published a partial total or per-path snapshot")
	}
}

func TestScratchAdmissionChargesGrowthDuringScan(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	scratch.stat = func(string) (int64, int64, error) {
		used, err := diskUsage(scratch.Root())
		return 1_000_000, 600_000 - used, err
	}
	writer, err := scratch.New("remux", 400_000)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(true)
	peer, err := scratch.New("peer", 140_000)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(true)
	scratch.usage = func(dir string) (scratchUsage, error) {
		if dir == writer.Dir {
			if err := os.WriteFile(filepath.Join(dir, "output.part"), make([]byte, 400_000), 0600); err != nil {
				return scratchUsage{}, err
			}
		}
		return scanDiskUsage(dir)
	}
	if err := peer.Reserve(300_000); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("admission ignored external bytes discovered during its scan: %v", err)
	}
	if peer.reserved != 140_000 {
		t.Fatalf("refused reservation changed the peer's promise: %d", peer.reserved)
	}
}

func TestScratchRenameKeepsLogicalFileCredits(t *testing.T) {
	for _, operation := range []string{"same path", "hard link", "symlink", "directory"} {
		t.Run(operation, func(t *testing.T) {
			scratch := NewScratch(t.TempDir())
			writer, err := scratch.New("capture", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close(true)
			source, target := filepath.Join(writer.Dir, "source"), filepath.Join(writer.Dir, "target")
			if operation == "directory" {
				if err := os.Mkdir(source, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, "input"), []byte("media"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(source, []byte("media"), 0600); err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "same path":
				target = source
			case "hard link":
				if err := os.Link(source, target); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(source, target); err != nil {
					t.Fatal(err)
				}
				source, target = target, filepath.Join(writer.Dir, "renamed-link")
			}
			if err := writer.Reserve(0); err != nil {
				t.Fatal(err)
			}
			err = writer.Rename(source, target)
			if operation == "directory" {
				if err == nil {
					t.Fatal("directory rename bypassed per-file accounting")
				}
				if body, err := os.ReadFile(filepath.Join(source, "input")); err != nil || string(body) != "media" {
					t.Fatalf("rejected directory rename moved recovery files: %q, %v", body, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if operation == "hard link" {
				if err := writer.Remove(source); err != nil {
					t.Fatal(err)
				}
				if body, err := os.ReadFile(target); err != nil || string(body) != "media" {
					t.Fatalf("same-inode rename lost the surviving link: %q, %v", body, err)
				}
			}
			actual, err := scanDiskUsage(writer.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(scratch.work[writer], actual) {
				t.Fatalf("rename credits differ from the logical file scan: cached=%+v actual=%+v", scratch.work[writer], actual)
			}
		})
	}
}

func TestScratchTruncateGrowthPreservesReservations(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	scratch.stat = func(string) (int64, int64, error) {
		used, err := diskUsage(scratch.Root())
		return 1_000_000, 600_000 - used, err
	}
	writer, err := scratch.New("capture", 400_000)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(true)
	peer, err := scratch.New("peer", 140_000)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(true)
	file, err := os.Create(filepath.Join(writer.Dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := writer.Truncate(file, 420_000); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("growing file consumed peer reservation: %v", err)
	}
	if info, err := file.Stat(); err != nil || info.Size() != 0 {
		t.Fatalf("refused growth changed file: %v, %v", info, err)
	}
	if err := writer.Truncate(file, 400_000); err != nil {
		t.Fatal(err)
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 0 {
		t.Fatalf("truncate moved writer offset: %d, %v", offset, err)
	}
	peerFile, err := os.Create(filepath.Join(peer.Dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer peerFile.Close()
	if n, err := peer.WriteFile(t.Context(), peerFile, make([]byte, 140_000)); n != 140_000 || err != nil {
		t.Fatalf("growth was charged twice: wrote=%d err=%v", n, err)
	}
}

func TestScratchStaleOwnerCannotMutateReopenedDirectory(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	writer, err := scratch.New("capture", 0)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(writer.Dir, "saved.ts"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("saved media"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(false); err != nil {
		t.Fatal(err)
	}
	current, err := scratch.Open(writer.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close(true)
	for name, mutate := range map[string]func() error{
		"rename":   func() error { return writer.Rename(file.Name(), filepath.Join(writer.Dir, "renamed")) },
		"remove":   func() error { return writer.Remove(file.Name()) },
		"truncate": func() error { return writer.Truncate(file, 0) },
		"write": func() error {
			_, err := writer.WriteFile(t.Context(), file, []byte("replaced"))
			return err
		},
		"reserve additional": func() error { return writer.ReserveAdditional(t.Context(), 100) },
		"create": func() error {
			created, err := writer.create(file.Name())
			if created != nil {
				created.Close()
			}
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !errors.Is(err, errWorkspaceClosed) {
				t.Fatalf("stale owner mutated recovery files: %v", err)
			}
		})
	}
	if body, err := os.ReadFile(file.Name()); err != nil || string(body) != "saved media" {
		t.Fatalf("stale owner changed current owner's media: %q, %v", body, err)
	}
}

func TestScratchMutationsRejectPathsOutsideWorkspace(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	writer, err := scratch.New("capture", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(true)
	inside := filepath.Join(writer.Dir, "saved.ts")
	if err := os.WriteFile(inside, []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	outside, err := os.Create(filepath.Join(scratch.Root(), "outside"))
	if err != nil {
		t.Fatal(err)
	}
	defer outside.Close()
	if _, err := outside.WriteString("outside"); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func() error{
		"rename out":  func() error { return writer.Rename(inside, outside.Name()) },
		"rename in":   func() error { return writer.Rename(outside.Name(), inside) },
		"remove":      func() error { return writer.Remove(outside.Name()) },
		"remove root": func() error { return writer.Remove(writer.Dir) },
		"truncate":    func() error { return writer.Truncate(outside, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); err == nil {
				t.Fatal("workspace changed a path outside its ownership")
			}
		})
	}
	if body, err := os.ReadFile(inside); err != nil || string(body) != "inside" {
		t.Fatalf("inside media changed: %q, %v", body, err)
	}
	if body, err := os.ReadFile(outside.Name()); err != nil || string(body) != "outside" {
		t.Fatalf("outside media changed: %q, %v", body, err)
	}
}
