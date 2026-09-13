// Package storage provides local and S3 object backends. External copies may
// mirror stored media; moving the originals makes storage scans mark recordings
// missing.
package storage

import (
	"context"
	"io"
	"time"
)

// FileInfo describes an object or directory returned by Stat.
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
	// Save copies all of r to path, creating parent directories as needed.
	// Atomic replacement is backend-dependent; an error may leave stored bytes.
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

// Reader is the object access given to consumers that cannot publish or delete.
type Reader interface {
	Open(context.Context, string) (io.ReadSeekCloser, error)
	Exists(context.Context, string) (bool, error)
	Stat(context.Context, string) (FileInfo, error)
}

// Writer permits publication without access to stored objects or deletion.
type Writer interface {
	Save(context.Context, string, io.Reader) error
}
