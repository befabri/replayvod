package mediastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

// Scratch reserves space on its own filesystem, independently of media storage.
// Bytes already on disk count toward free-space usage instead of future reservations.
// New, Open, and Workspace.Reserve may temporarily refuse admission while external files grow.
type Scratch struct {
	root string
	mu   sync.Mutex
	// work totals and per-path credits change together under mu.
	work  map[*Workspace]scratchUsage
	stat  func(string) (int64, int64, error)
	usage func(string) (scratchUsage, error)
}

type scratchUsage struct {
	bytes int64
	files map[string]int64
}

// Workspace owns a scratch directory and its capacity reservation until Close.
// Callers must use its file mutation methods and stop writers and Monitor before Close.
// External writers must stop before their files are renamed, removed, or truncated.
type Workspace struct {
	owner    *Scratch
	Dir      string
	reserved int64
	cancel   context.CancelCauseFunc
}

// ErrWorkspaceBusy means an overlapping scratch directory already has an owner.
var ErrWorkspaceBusy = errors.New("scratch workspace already owned")

// NewScratch creates accounting for a nonempty root; an empty root panics.
func NewScratch(root string) *Scratch {
	if root == "" {
		panic("scratch directory required")
	}
	return &Scratch{root: root, work: map[*Workspace]scratchUsage{}, stat: scratchStat, usage: scanDiskUsage}
}
func scratchStat(root string) (int64, int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), nil
}

// New creates a temporary workspace with estimate bytes reserved.
func (s *Scratch) New(prefix string, estimate int64) (*Workspace, error) {
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(s.root, prefix+"-")
	if err != nil {
		return nil, err
	}
	w, err := s.Open(dir, estimate)
	if err != nil {
		_ = os.RemoveAll(dir)
	}
	return w, err
}

// Open takes ownership of a recording's stable recovery directory.
func (s *Scratch) Open(dir string, estimate int64) (*Workspace, error) {
	return s.open(dir, estimate, false)
}

// OpenForCleanup owns an abandoned directory without creating or reserving it.
// Close(false) preserves recovery files if settlement fails.
func (s *Scratch) OpenForCleanup(dir string) (*Workspace, error) {
	return s.open(dir, 0, true)
}

func (s *Scratch) open(dir string, estimate int64, cleanup bool) (*Workspace, error) {
	root, err := filepath.Abs(s.root)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("workspace must be inside configured scratch directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for w := range s.work {
		if w.Dir == abs || strings.HasPrefix(abs, w.Dir+string(filepath.Separator)) || strings.HasPrefix(w.Dir, abs+string(filepath.Separator)) {
			return nil, ErrWorkspaceBusy
		}
	}
	if !cleanup {
		if err := os.MkdirAll(abs, 0755); err != nil {
			return nil, err
		}
	}
	w := &Workspace{owner: s, Dir: abs, reserved: max(estimate, 0)}
	s.work[w] = scratchUsage{}
	if !cleanup {
		if err := s.checkLocked(); err != nil {
			delete(s.work, w)
			return nil, err
		}
	}
	return w, nil
}

var errScratchScanChanged = errors.New("scratch changed during accounting scan")

// scratchScanAttempts bounds retries after entries disappear during accounting;
// skipping those entries would count already-written bytes as future growth.
const scratchScanAttempts = 64

func scanDiskUsage(dir string) (scratchUsage, error) {
	return scanFilesystemUsage(os.DirFS(dir), dir)
}

func scanFilesystemUsage(root fs.FS, dir string) (scratchUsage, error) {
	for range scratchScanAttempts {
		usage := scratchUsage{files: make(map[string]int64)}
		err := fs.WalkDir(root, ".", func(path string, e fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				if path == "." { // A removed workspace has no remaining files.
					return nil
				}
				return errScratchScanChanged
			}
			if err != nil {
				return err
			}
			if e.Type().IsRegular() {
				info, err := e.Info()
				if errors.Is(err, fs.ErrNotExist) {
					return errScratchScanChanged
				}
				if err != nil {
					return err
				}
				usage.bytes += info.Size()
				usage.files[filepath.Join(dir, path)] = info.Size()
			}
			return nil
		})
		if errors.Is(err, errScratchScanChanged) {
			continue
		}
		if err != nil {
			return scratchUsage{}, err
		}
		return usage, nil
	}
	return scratchUsage{}, errScratchScanChanged
}
func (s *Scratch) checkLocked() error {
	usage, err := s.scanLocked()
	if err != nil {
		return err
	}
	return s.checkCapacityLocked(usage, nil, 0)
}

func (s *Scratch) scanLocked() (map[*Workspace]scratchUsage, error) {
	usage := make(map[*Workspace]scratchUsage, len(s.work))
	for w := range s.work {
		used, err := s.usage(w.Dir)
		if err != nil {
			return nil, err
		}
		usage[w] = used
	}
	return usage, nil
}

// scratchCapacityAttempts bounds work under the accounting lock while ffmpeg grows.
const (
	scratchCapacityAttempts = 2
	scratchRetryDelay       = 10 * time.Millisecond
)

func (s *Scratch) checkCapacityLocked(usage map[*Workspace]scratchUsage, writer *Workspace, growth int64) error {
	for range scratchCapacityAttempts {
		required := growth + pendingScratchBytes(usage, writer)
		// Read free space last so external growth is charged before its bytes are credited.
		total, avail, err := s.stat(s.root)
		if err != nil {
			return err
		}
		if avail < total/20 {
			return fmt.Errorf("%w: scratch space below filesystem reserve", storage.ErrFull)
		}
		available := avail - total/20
		if required <= available {
			// Writers must not see mixed accounting snapshots from a partial scan.
			s.work = usage
			return nil
		}
		refreshed, err := s.scanLocked()
		if err != nil {
			return err
		}
		// A falling pending balance may already have consumed the space sampled by statfs.
		if growth+pendingScratchBytes(refreshed, writer) >= required {
			s.work = refreshed
			return fmt.Errorf("%w: scratch requires %d bytes, %d available after reserve", storage.ErrFull, required, available)
		}
		usage = refreshed
	}
	return errScratchScanChanged
}

func pendingScratchBytes(usage map[*Workspace]scratchUsage, writer *Workspace) int64 {
	var pending int64
	for workspace, used := range usage {
		if workspace != writer {
			pending += max(workspace.reserved-used.bytes, 0)
		}
	}
	return pending
}

// Reserve raises the total expected workspace footprint before a new writer.
func (w *Workspace) Reserve(bytes int64) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	return w.reserveLocked(bytes)
}

func (w *Workspace) reserveLocked(bytes int64) error {
	s := w.owner
	if _, ok := s.work[w]; !ok {
		return errWorkspaceClosed
	}
	old := w.reserved
	w.reserved = max(w.reserved, bytes)
	if err := s.checkLocked(); err != nil {
		w.reserved = old
		return err
	}
	return nil
}

// Close releases ownership, optionally removing files first. Removal failure
// retains ownership so the caller can retry.
func (w *Workspace) Close(remove bool) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, owned := s.work[w]; !owned {
		return nil
	}
	if remove {
		if err := os.RemoveAll(w.Dir); err != nil {
			return err
		}
	}
	delete(s.work, w)
	return nil
}

// Monitor accounts for growing live capture and external ffmpeg outputs. Its
// cancel function joins the monitor, and must run before releasing the workspace.
func (w *Workspace) Monitor(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	w.owner.mu.Lock()
	w.cancel = cancel
	w.owner.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s := w.owner
				s.mu.Lock()
				err := s.checkLocked()
				s.mu.Unlock()
				if errors.Is(err, errScratchScanChanged) {
					continue // Churn is not evidence of a full filesystem.
				}
				if err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled); <-done }
}

// Copy stages an object and removes partial output on a copy error.
func (w *Workspace) Copy(ctx context.Context, store *Store, key, name string) (string, error) {
	src, err := store.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer src.Close()
	path := filepath.Join(w.Dir, filepath.Base(name))
	dst, err := w.create(path)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(&scratchWriter{ctx: ctx, w: w, file: dst}, src)
	closeErr := dst.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = w.Remove(path)
		return "", err
	}
	if err := store.Verify(ctx); !storage.CanRead(err) {
		return "", err
	}
	return path, nil
}

type scratchWriter struct {
	ctx  context.Context
	w    *Workspace
	file *os.File
}

func (b *scratchWriter) Write(p []byte) (int, error) { return b.w.WriteFile(b.ctx, b.file, p) }

// WriteFile accounts for each write while preserving other workspace reservations.
// It waits for changing capacity snapshots to settle or ctx to be canceled.
// The file must belong to this workspace; confirmed failures cancel its monitor.
func (w *Workspace) WriteFile(ctx context.Context, file *os.File, p []byte) (int, error) {
	s := w.owner
	for {
		s.mu.Lock()
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return 0, err
		}
		path, err := w.pathLocked(file.Name())
		if err != nil {
			s.mu.Unlock()
			return 0, err
		}
		if err := w.checkGrowthLocked(int64(len(p))); err != nil {
			retry := errors.Is(err, errScratchScanChanged)
			if !retry && w.cancel != nil {
				w.cancel(err)
			}
			s.mu.Unlock()
			if !retry {
				return 0, err
			}
			if err := waitScratchRetry(ctx); err != nil {
				return 0, err
			}
			continue
		}
		n, err := file.Write(p)
		err = errors.Join(err, w.refreshFileUsageLocked(path))
		if err != nil && w.cancel != nil {
			w.cancel(err)
		}
		s.mu.Unlock()
		return n, err
	}
}

// Root returns the configured scratch location.
func (s *Scratch) Root() string { return s.root }

// ReserveAdditional reserves bytes beyond the usage observed at the start of the call.
// It retries changing capacity snapshots until they settle or ctx is canceled.
func (w *Workspace) ReserveAdditional(ctx context.Context, bytes int64) error {
	s := w.owner
	var target int64
	initialized := false
	for {
		err := func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, owned := s.work[w]; !owned {
				return errWorkspaceClosed
			}
			if !initialized {
				used, err := s.usage(w.Dir)
				if err != nil {
					return err
				}
				// Growth during retries must consume the original target.
				target = used.bytes + max(bytes, 0)
				initialized = true
			}
			return w.reserveLocked(target)
		}()
		if !errors.Is(err, errScratchScanChanged) {
			return err
		}
		if err := waitScratchRetry(ctx); err != nil {
			return err
		}
	}
}

func waitScratchRetry(ctx context.Context) error {
	timer := time.NewTimer(scratchRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
