package mediastore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/befabri/replayvod/server/internal/storage"
)

var errWorkspaceClosed = errors.New("scratch workspace closed")

func (w *Workspace) pathLocked(path string) (string, error) {
	if _, owned := w.owner.work[w]; !owned {
		return "", errWorkspaceClosed
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.Dir, abs)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("file must be inside its scratch workspace")
	}
	return abs, nil
}

func (w *Workspace) checkGrowthLocked(bytes int64) error {
	s := w.owner
	total, avail, err := s.stat(s.root)
	if err != nil {
		return err
	}
	var pending int64
	for other, used := range s.work {
		if other != w {
			pending += max(other.reserved-used.bytes, 0)
		}
	}
	if bytes+pending > max(avail-total/20, 0) {
		return fmt.Errorf("%w: scratch write", storage.ErrFull)
	}
	return nil
}

// Rename moves a file within the workspace without racing accounting scans.
// Directory renames are unsupported; replacing a file restores its reservation.
func (w *Workspace) Rename(oldPath, newPath string) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	oldPath, err := w.pathLocked(oldPath)
	if err != nil {
		return err
	}
	newPath, err = w.pathLocked(newPath)
	if err != nil {
		return err
	}
	source, err := os.Lstat(oldPath)
	if err != nil {
		return err
	}
	if source.IsDir() {
		return fmt.Errorf("scratch rename requires a file")
	}
	target, err := os.Lstat(newPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(oldPath, newPath); err != nil {
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
	path, err := w.pathLocked(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.setFileUsageLocked(path, 0)
		}
		return err
	}
	if err := os.Remove(path); err != nil {
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
func (w *Workspace) Truncate(file *os.File, size int64) error {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := w.pathLocked(file.Name()); err != nil {
		return err
	}
	return w.truncateLocked(file, size)
}

func (w *Workspace) truncateLocked(file *os.File, size int64) error {
	before, err := file.Stat()
	if err != nil {
		return err
	}
	if size > before.Size() {
		if err := w.checkGrowthLocked(size - before.Size()); err != nil {
			if w.cancel != nil {
				w.cancel(err)
			}
			return err
		}
	}
	if err := file.Truncate(size); err != nil {
		return err
	}
	path, err := filepath.Abs(file.Name())
	if err != nil {
		return err
	}
	return w.refreshFileUsageLocked(path)
}

func regularFileSize(info os.FileInfo) int64 {
	if info.Mode().IsRegular() {
		return info.Size()
	}
	return 0
}

func (w *Workspace) refreshFileUsageLocked(path string) error {
	info, err := os.Lstat(path)
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
	path, err := w.pathLocked(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}
	if err := w.truncateLocked(file, 0); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
