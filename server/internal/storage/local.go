package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	Root string
}

// NewLocal never creates the root: an absent directory is an unmounted volume
// until the first attach (WriteMarker) creates it deliberately.
func NewLocal(dir string) (*LocalStorage, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("abs storage root: %w", err)
	}
	return &LocalStorage{Root: abs}, nil
}

// resolve maps a forward-slash relative path to an absolute local path and
// rejects attempts to escape the storage root (e.g. "../../etc/passwd").
func (s *LocalStorage) resolve(p string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(p))
	if cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path escapes storage root: %s", p)
	}
	return filepath.Join(s.Root, cleaned), nil
}

// Save writes r to path atomically: copy to a private temporary file, then rename.
// All operations use the same open root, so a mount replacement cannot redirect
// a write or its cleanup midway. Only identity initialization may create a root.
func (s *LocalStorage) Save(ctx context.Context, path string, r io.Reader) error {
	full, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == MarkerPath {
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return fmt.Errorf("initialize storage root: %w", err)
		}
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return fmt.Errorf("open storage root: %w", err)
	}
	defer root.Close()
	rel, err := filepath.Rel(s.Root, full)
	if err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}

	// Writers for the same object must never share a temporary inode: one
	// writer could publish it while another is still changing its contents.
	id, err := NewStorageID()
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(rel), ".replayvod-save-"+id+".tmp")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}

	if _, err := copyContext(ctx, f, r); err != nil {
		f.Close()
		root.Remove(tmp)
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		root.Remove(tmp)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		root.Remove(tmp)
		return err
	}

	if err := root.Rename(tmp, rel); err != nil {
		root.Remove(tmp)
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}

func (s *LocalStorage) Open(ctx context.Context, path string) (io.ReadSeekCloser, error) {
	full, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *LocalStorage) Delete(ctx context.Context, path string) error {
	full, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove file: %w", err)
	}
	return nil
}

func (s *LocalStorage) Exists(ctx context.Context, path string) (bool, error) {
	full, err := s.resolve(path)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

func (s *LocalStorage) Stat(ctx context.Context, path string) (FileInfo, error) {
	full, err := s.resolve(path)
	if err != nil {
		return FileInfo{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

// LocalPath returns the absolute filesystem path for a stored object.
// Only meaningful for LocalStorage — callers that use it commit to the
// local backend. Used by the byte-range streaming handler which wants to
// hand the file path directly to http.ServeFile.
func (s *LocalStorage) LocalPath(path string) (string, error) {
	return s.resolve(path)
}

// copyContext is io.Copy that honors ctx cancellation. Large video copies
// need this so a shutdown actually stops the transfer.
func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 64*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// ProbeRoot checks that the root is a reachable directory. Whether it is the
// right directory is the identity marker's job: a volume swapped under a
// running process reads as unattached until it carries the marker, and can be
// adopted without a restart.
func (s *LocalStorage) ProbeRoot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(s.Root)
	if err != nil {
		return fmt.Errorf("storage root unreachable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("storage root unreachable: %s is not a directory", s.Root)
	}
	return nil
}

// ProbeWrite creates, writes, syncs and removes a small file under one open
// root. Creating an empty file alone can succeed on a disk with no data space.
func (s *LocalStorage) ProbeWrite(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return fmt.Errorf("storage root not writable: %w", err)
	}
	defer root.Close()
	id, err := NewStorageID()
	if err != nil {
		return err
	}
	name := ".replayvod-write-probe-" + id
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("storage root not writable: %w", err)
	}
	_, probeErr := f.Write([]byte{1})
	if probeErr == nil {
		probeErr = f.Sync()
	}
	if err := f.Close(); probeErr == nil {
		probeErr = err
	}
	if err := root.Remove(name); probeErr == nil {
		probeErr = err
	}
	if probeErr != nil {
		return fmt.Errorf("storage root not writable: %w", probeErr)
	}
	return nil
}
