package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

// RcloneStorage is a storage backend that shells out to the `rclone`
// binary. Useful for archival tiering: point Remote at a configured
// rclone remote (e.g. "archive:replayvod/videos") and every supported
// rclone provider (S3, B2, GDrive, SFTP, local, etc.) works.
//
// Tradeoffs:
//   - Save is O(stream) — pipe from stdin through `rclone rcat`.
//   - Open is slow-path: `rclone cat` streams the full object into a
//     tempfile, then we return a *os.File so http.ServeContent can
//     seek. Fine for thumbnails and one-off playback; avoid for
//     concurrent many-client streaming. Operators who want
//     stream-playback performance should pick the S3 backend.
//   - Delete / Stat / Exists call `rclone delete`, `rclone lsjson`.
type RcloneStorage struct {
	Binary string // "rclone" by default
	Remote string // e.g. "archive:bucket/path"
}

// NewRclone builds the backend. remote must be non-empty; the form is
// what `rclone` accepts ("<remote>:<path>"). The function does NOT
// verify the remote exists — first real operation surfaces any error
// so a misconfigured dashboard can still boot.
func NewRclone(binary, remote string) (*RcloneStorage, error) {
	if remote == "" {
		return nil, fmt.Errorf("rclone storage: remote required")
	}
	if binary == "" {
		binary = "rclone"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("rclone storage: binary %q not on PATH: %w", binary, err)
	}
	return &RcloneStorage{Binary: binary, Remote: remote}, nil
}

// joinRemote attaches a relative path to the configured remote root.
// Uses path (not filepath) so separators stay forward-slash across OS.
func (r *RcloneStorage) joinRemote(p string) (string, error) {
	key, err := objectKey(p)
	if err != nil {
		return "", err
	}
	// Remote already includes a colon and optional path — we append
	// with a slash only when the remote doesn't end in one.
	if strings.HasSuffix(r.Remote, "/") || strings.HasSuffix(r.Remote, ":") {
		return r.Remote + key, nil
	}
	return r.Remote + "/" + key, nil
}

func (r *RcloneStorage) Save(ctx context.Context, p string, src io.Reader) error {
	target, err := r.joinRemote(p)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, r.Binary, "rcat", target)
	cmd.Stdin = src
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone rcat %s: %w (stderr: %s)", target, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *RcloneStorage) Open(ctx context.Context, p string) (io.ReadSeekCloser, error) {
	target, err := r.joinRemote(p)
	if err != nil {
		return nil, err
	}

	exists, statErr := r.Exists(ctx, p)
	if statErr != nil {
		return nil, statErr
	}
	if !exists {
		return nil, errNotFound{key: target}
	}

	// Buffer the object into a tempfile so the returned handle supports
	// Seek. rclone's `cat` can stream ranges via --offset/--count, but
	// stitching that into a Seeker is more machinery than it's worth
	// for a backend that's primarily for archival.
	tmp, err := os.CreateTemp("", "replayvod-rclone-*")
	if err != nil {
		return nil, fmt.Errorf("rclone: create tempfile: %w", err)
	}
	cmd := exec.CommandContext(ctx, r.Binary, "cat", target)
	cmd.Stdout = tmp
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, fmt.Errorf("rclone cat %s: %w (stderr: %s)", target, err, strings.TrimSpace(stderr.String()))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, fmt.Errorf("rclone: rewind tempfile: %w", err)
	}
	return &rcloneTempHandle{File: tmp}, nil
}

// rcloneTempHandle wraps *os.File with a Close that also removes the
// backing tempfile. Without this the caller would leak a file per
// Open call — the cleanup has to happen when the caller is done
// reading, not when the command exits.
type rcloneTempHandle struct {
	*os.File
}

func (h *rcloneTempHandle) Close() error {
	name := h.File.Name()
	err := h.File.Close()
	if rmErr := os.Remove(name); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		if err == nil {
			err = rmErr
		}
	}
	return err
}

func (r *RcloneStorage) Delete(ctx context.Context, p string) error {
	// Probe with Stat first: lsjson's "missing" signal is structured
	// and stable across rclone versions, whereas deletefile's stderr
	// phrasing drifted historically ("object not found" vs
	// "directory not found" vs newer variants). Idempotent Delete is
	// a Storage contract — callers rely on it for cleanup retries.
	if _, err := r.Stat(ctx, p); err != nil {
		if errors.As(err, new(errNotFound)) {
			return nil
		}
		return err
	}
	target, err := r.joinRemote(p)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, r.Binary, "deletefile", target)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone deletefile %s: %w (stderr: %s)", target, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *RcloneStorage) Exists(ctx context.Context, p string) (bool, error) {
	info, err := r.Stat(ctx, p)
	if err != nil {
		if errors.As(err, new(errNotFound)) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir, nil
}

// lsjsonEntry is the subset of `rclone lsjson` we read. rclone returns
// a JSON array; we call it with --max-depth 1 on the parent so we can
// find a single name without walking a whole tree.
type lsjsonEntry struct {
	Name    string    `json:"Name"`
	Path    string    `json:"Path"`
	Size    int64     `json:"Size"`
	ModTime time.Time `json:"ModTime"`
	IsDir   bool      `json:"IsDir"`
}

func (r *RcloneStorage) Stat(ctx context.Context, p string) (FileInfo, error) {
	key, err := objectKey(p)
	if err != nil {
		return FileInfo{}, err
	}
	parent, base := path.Split(key)
	// Build the parent target path. Trim trailing slash — rclone wants
	// the directory without it.
	parent = strings.TrimRight(parent, "/")
	var parentTarget string
	if parent == "" {
		parentTarget = r.Remote
	} else if strings.HasSuffix(r.Remote, "/") || strings.HasSuffix(r.Remote, ":") {
		parentTarget = r.Remote + parent
	} else {
		parentTarget = r.Remote + "/" + parent
	}

	cmd := exec.CommandContext(ctx, r.Binary, "lsjson", "--max-depth", "1", "--files-only", parentTarget)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Directory-not-found behaves like the object not existing.
		if strings.Contains(stderr.String(), "directory not found") {
			return FileInfo{}, errNotFound{key: key}
		}
		return FileInfo{}, fmt.Errorf("rclone lsjson %s: %w (stderr: %s)", parentTarget, err, strings.TrimSpace(stderr.String()))
	}
	var entries []lsjsonEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return FileInfo{}, fmt.Errorf("rclone lsjson parse: %w", err)
	}
	for _, e := range entries {
		if e.Name == base {
			return FileInfo{Size: e.Size, ModTime: e.ModTime, IsDir: e.IsDir}, nil
		}
	}
	return FileInfo{}, errNotFound{key: key}
}

// compile-time interface check.
var _ Storage = (*RcloneStorage)(nil)
