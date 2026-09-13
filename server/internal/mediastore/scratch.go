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
type Scratch struct {
	root string
	mu   sync.Mutex
	work map[*Workspace]int64
	stat func(string) (int64, int64, error)
}

// Workspace owns a scratch directory and its capacity reservation until Close.
// Callers must stop writers and join Monitor before releasing it.
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
	return &Scratch{root: root, work: map[*Workspace]int64{}, stat: scratchStat}
}
func scratchStat(root string) (int64, int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), nil
}

// New creates an owned temporary workspace with estimate bytes reserved; the
// caller must close it after all writers finish.
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
	s.work[w] = 0
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

func diskUsage(dir string) (int64, error) {
	return filesystemUsage(os.DirFS(dir))
}

func filesystemUsage(root fs.FS) (int64, error) {
	for range scratchScanAttempts {
		var size int64
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
				size += info.Size()
			}
			return nil
		})
		if errors.Is(err, errScratchScanChanged) {
			continue
		}
		if err != nil {
			return 0, err
		}
		return size, nil
	}
	return 0, errScratchScanChanged
}
func (s *Scratch) checkLocked() error {
	total, avail, err := s.stat(s.root)
	if err != nil {
		return err
	}
	if avail < total/20 {
		return fmt.Errorf("%w: scratch space below filesystem reserve", storage.ErrFull)
	}
	var remaining int64
	usage := make(map[*Workspace]int64, len(s.work))
	for w := range s.work {
		used, err := diskUsage(w.Dir)
		if err != nil {
			return err
		}
		usage[w] = used
		remaining += max(w.reserved-used, 0)
	}
	// Per-chunk writers use this cache too. Never publish a partial scan to
	// them, even when another workspace prevents this check from completing.
	s.work = usage
	if remaining > max(avail-total/20, 0) {
		return fmt.Errorf("%w: scratch requires %d bytes, %d available after reserve", storage.ErrFull, remaining, max(avail-total/20, 0))
	}
	return nil
}

// Reserve raises the total expected workspace footprint before a new writer.
func (w *Workspace) Reserve(bytes int64) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.work[w]; !ok {
		return fmt.Errorf("scratch workspace closed")
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

// Copy stages an object in the workspace, reserving capacity before each write.
func (w *Workspace) Copy(ctx context.Context, store *Store, key, name string) (string, error) {
	src, err := store.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer src.Close()
	path := filepath.Join(w.Dir, filepath.Base(name))
	dst, err := os.Create(path)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(&scratchWriter{ctx: ctx, w: w, file: dst}, src)
	closeErr := dst.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = os.Remove(path)
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
// file must belong to this workspace; capacity refusals cancel its monitor.
func (w *Workspace) WriteFile(ctx context.Context, file *os.File, p []byte) (n int, err error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	total, avail, err := s.stat(s.root)
	if err != nil {
		return 0, err
	}
	current := w
	if _, ok := s.work[current]; !ok {
		return 0, fmt.Errorf("scratch workspace closed")
	}
	defer func() {
		if err != nil && current.cancel != nil {
			current.cancel(err)
		}
	}()
	var pending int64
	for w, used := range s.work {
		if w != current {
			pending += max(w.reserved-used, 0)
		}
	}
	if int64(len(p))+pending > max(avail-total/20, 0) {
		return 0, fmt.Errorf("%w: scratch write", storage.ErrFull)
	}
	n, err = file.Write(p)
	s.work[current] += int64(n)
	return n, err
}

// Root returns the configured scratch location.
func (s *Scratch) Root() string { return s.root }

// ReserveAdditional reserves bytes beyond the workspace's current disk usage.
func (w *Workspace) ReserveAdditional(bytes int64) error {
	used, err := diskUsage(w.Dir)
	if err != nil {
		return err
	}
	return w.Reserve(used + max(bytes, 0))
}
