package mediastore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	errWorkspaceClosed  = errors.New("scratch workspace closed")
	errCleanupWorkspace = errors.New("scratch workspace opened only for cleanup")
)

// ErrOutsideWorkspace is returned for a path that does not name a file inside
// the workspace.
var ErrOutsideWorkspace = errors.New("file must be inside its scratch workspace")

// pathLocked returns the absolute path that keys accounting and the path
// relative to the workspace root.
func (w *Workspace) pathLocked(path string) (string, string, error) {
	if _, owned := w.owner.work[w]; !owned {
		return "", "", errWorkspaceClosed
	}
	if w.root == nil {
		return "", "", errCleanupWorkspace
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(w.Dir, abs)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", "", ErrOutsideWorkspace
	}
	return abs, rel, nil
}

// Open opens a workspace file for reading. It refuses paths and links that lead
// outside the workspace.
func (w *Workspace) Open(path string) (*os.File, error) {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	_, rel, err := w.pathLocked(path)
	if err != nil {
		return nil, err
	}
	return w.root.Open(rel)
}

func (w *Workspace) checkGrowthLocked(bytes int64) error {
	return w.owner.checkCapacityLocked(w.owner.work, w, bytes)
}

// Rename moves a file within the workspace without racing accounting scans.
// Directory renames are unsupported; replacing a file restores its reservation.
func (w *Workspace) Rename(oldPath, newPath string) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	oldPath, oldRel, err := w.pathLocked(oldPath)
	if err != nil {
		return err
	}
	newPath, newRel, err := w.pathLocked(newPath)
	if err != nil {
		return err
	}
	source, err := w.root.Lstat(oldRel)
	if err != nil {
		return err
	}
	if source.IsDir() {
		return fmt.Errorf("scratch rename requires a file")
	}
	target, err := w.root.Lstat(newRel)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := w.root.Rename(oldRel, newRel); err != nil {
		return err
	}
	if target != nil && os.SameFile(source, target) {
		w.setFileUsageLocked(oldPath, regularFileSize(source))
		w.setFileUsageLocked(newPath, regularFileSize(target))
		return nil
	}
	w.setFileUsageLocked(oldPath, 0)
	w.setFileUsageLocked(newPath, regularFileSize(source))
	return nil
}

// Remove deletes a file or empty directory and restores its unwritten reservation.
func (w *Workspace) Remove(path string) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	path, rel, err := w.pathLocked(path)
	if err != nil {
		return err
	}
	info, err := w.root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.setFileUsageLocked(path, 0)
		}
		return err
	}
	if err := w.root.Remove(rel); err != nil {
		return err
	}
	w.setFileUsageLocked(path, 0)
	if info.IsDir() {
		for file := range s.work[w].files {
			if strings.HasPrefix(file, path+string(filepath.Separator)) {
				w.setFileUsageLocked(file, 0)
			}
		}
	}
	return nil
}

// Truncate changes a workspace file's size while preserving other reservations.
// It leaves the file offset unchanged, as os.File.Truncate does.
// Growth can return a temporary accounting error without canceling the monitor.
func (w *Workspace) Truncate(file *os.File, size int64) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	path, rel, err := w.pathLocked(file.Name())
	if err != nil {
		return err
	}
	return w.truncateLocked(file, path, rel, size)
}

func (w *Workspace) truncateLocked(file *os.File, path, rel string, size int64) error {
	before, err := file.Stat()
	if err != nil {
		return err
	}
	if size > before.Size() {
		if err := w.checkGrowthLocked(size - before.Size()); err != nil {
			if !errors.Is(err, errScratchScanChanged) && w.cancel != nil {
				w.cancel(err)
			}
			return err
		}
	}
	if err := file.Truncate(size); err != nil {
		return err
	}
	return w.refreshFileUsageLocked(path, rel)
}

func regularFileSize(info os.FileInfo) int64 {
	if info.Mode().IsRegular() {
		return info.Size()
	}
	return 0
}

func (w *Workspace) refreshFileUsageLocked(path, rel string) error {
	info, err := w.root.Lstat(rel)
	if err != nil {
		w.setFileUsageLocked(path, 0)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	w.setFileUsageLocked(path, regularFileSize(info))
	return nil
}

func (w *Workspace) setFileUsageLocked(path string, size int64) {
	usage := w.owner.work[w]
	// ffmpeg can add unscanned bytes; replace this path's credit without charging other files.
	usage.bytes += size - usage.files[path]
	if size == 0 {
		delete(usage.files, path)
	} else {
		if usage.files == nil {
			usage.files = make(map[string]int64)
		}
		usage.files[path] = size
	}
	w.owner.work[w] = usage
}

func (w *Workspace) create(path string) (*os.File, error) {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	path, rel, err := w.pathLocked(path)
	if err != nil {
		return nil, err
	}
	file, err := w.root.OpenFile(rel, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}
	if err := w.truncateLocked(file, path, rel, 0); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
