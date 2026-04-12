package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage stores files under Root on the local filesystem.
// All paths passed to Storage methods are treated as relative to Root.
type LocalStorage struct {
	Root string
}

// NewLocal creates a local filesystem storage backend rooted at dir.
// The directory is created if it does not already exist.
func NewLocal(dir string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage root %s: %w", dir, err)
	}
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

// Save writes r to path atomically: copy to "<final>.tmp" first, then rename.
// On rename failure the temp file is removed so we never leak partial writes.
func (s *LocalStorage) Save(ctx context.Context, path string, r io.Reader) error {
	full, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}

	tmp := full + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}

	if _, err := copyContext(ctx, f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmp, full); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}

// Open returns the file for random-access reads. Used by the video streaming
// handler where http.ServeContent wants a ReadSeeker.
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

// Delete removes the file. Missing files are not an error so repeated
// cleanup attempts are safe.
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
