// Package storage provides a backend-agnostic object store for video files
// and thumbnails. The Storage interface has one implementation per backend
// (local FS; S3-compatible for remote). Operators who want a copy of their
// recordings on a non-S3 target (Google Drive, SFTP, Backblaze, etc.) run an
// external rclone copy or sync from the local data directory. Media moved out
// of that directory reads as missing to the storage scan, which tombstones the
// recording.
package storage

import (
	"context"
	"io"
	"time"
)

type FileInfo struct {
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// Storage is the common interface for all backends.
//
// Paths are always forward-slash separated, rooted at the backend's base
// location. Implementations are responsible for mapping them to their
// native conventions (absolute path for local, key for S3).
type Storage interface {
	// Save writes r to path, creating parent directories as needed. The full
	// read is copied; callers typically hand this a file or a subprocess
	// stdout pipe. Atomicity is best-effort: for local FS we write to a
	// temp file and rename, but other backends may not guarantee it.
	Save(ctx context.Context, path string, r io.Reader) error

	// Open returns a ReadSeekCloser so HTTP range requests can serve video
	// files efficiently. The caller must Close() when done.
	Open(ctx context.Context, path string) (io.ReadSeekCloser, error)

	// Delete removes a single object. Missing objects return nil; deletion
	// is idempotent so cleanup retries are safe.
	Delete(ctx context.Context, path string) error

	// Exists reports whether path points at an object (not a directory).
	Exists(ctx context.Context, path string) (bool, error)

	// Stat returns metadata for path. Returns an os.ErrNotExist-compatible
	// error when the object is missing.
	Stat(ctx context.Context, path string) (FileInfo, error)
}

// RootProber is implemented by backends that can check their root location is
// reachable. The storage scan tombstones nothing while the probe fails, so a
// failed probe cannot read as a vanished library. A successful reachability
// probe does not authenticate the configured storage location. Backends without
// this capability cannot automatically tombstone recordings.
type RootProber interface {
	ProbeRoot(ctx context.Context) error
}
