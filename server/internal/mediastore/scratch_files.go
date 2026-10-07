package mediastore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// openRootLocked constrains filesystem operations to this workspace, including
// symlink resolution. Opening it through the scratch root also prevents an
// existing recovery-directory symlink from redirecting operations outside it.
func (w *Workspace) openRootLocked() (*os.Root, error) {
	if _, owned := w.owner.work[w]; !owned {
		return nil, errWorkspaceClosed
	}
	abs, err := filepath.Abs(w.owner.root)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(abs, w.Dir)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("workspace must be inside configured scratch directory")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("scratch workspace must be a directory")
	}
	workspace, err := root.OpenRoot(rel)
	if err != nil {
		return nil, err
	}
	opened, err := workspace.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		workspace.Close()
		return nil, fmt.Errorf("scratch workspace changed while opening")
	}
	return workspace, nil
}

func (w *Workspace) relativePathLocked(path string) (string, error) {
	abs, err := w.pathLocked(path)
	if err != nil {
		return "", err
	}
	return filepath.Rel(w.Dir, abs)
}

// filePathLocked validates caller-opened files before writes or truncation.
// A lexical name alone does not establish ownership: it can be a symlink to
// an external file, or can have been replaced since the handle was opened.
func (w *Workspace) filePathLocked(file *os.File) (string, error) {
	path, err := w.pathLocked(file.Name())
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.Dir, path)
	if err != nil {
		return "", err
	}
	root, err := w.openRootLocked()
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Stat(rel)
	if err != nil {
		return "", err
	}
	opened, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("file does not belong to its scratch workspace")
	}
	return path, nil
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
	oldPath, err := w.pathLocked(oldPath)
	if err != nil {
		return err
	}
	newPath, err = w.pathLocked(newPath)
	if err != nil {
		return err
	}
	root, err := w.openRootLocked()
	if err != nil {
		return err
	}
	defer root.Close()
	oldRel, err := filepath.Rel(w.Dir, oldPath)
	if err != nil {
		return err
	}
	newRel, err := filepath.Rel(w.Dir, newPath)
	if err != nil {
		return err
	}
	source, err := root.Lstat(oldRel)
	if err != nil {
		return err
	}
	if source.IsDir() {
		return fmt.Errorf("scratch rename requires a file")
	}
	target, err := root.Lstat(newRel)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(oldRel, newRel); err != nil {
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
	root, err := w.openRootLocked()
	if err != nil {
		return err
	}
	defer root.Close()
	rel, err := filepath.Rel(w.Dir, path)
	if err != nil {
		return err
	}
	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.setFileUsageLocked(path, 0)
		}
		return err
	}
	if err := root.Remove(rel); err != nil {
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
	if _, err := w.filePathLocked(file); err != nil {
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
			if !errors.Is(err, errScratchScanChanged) && w.cancel != nil {
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
	rel, err := w.relativePathLocked(path)
	if err != nil {
		return err
	}
	root, err := w.openRootLocked()
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(rel)
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
	root, err := w.openRootLocked()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	rel, err := filepath.Rel(w.Dir, path)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(rel, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}
	if err := w.truncateLocked(file, 0); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
