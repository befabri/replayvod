package hls

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PartWriter writes a single segment to disk using the .part →
// fsync → atomic rename pattern. A crash or SIGKILL during the
// write leaves a .part file behind, which the startup sweep
// deletes. Files without the .part suffix are always complete and
// safe to read.
//
// The zero value is not usable; construct with NewPartWriter.
//
// Usage:
//
//	w, err := NewPartWriter(dir, "42.ts")
//	if err != nil { ... }
//	defer w.Abort() // idempotent; no-op after Commit
//	if _, err := io.Copy(w, body); err != nil { return err }
//	if err := w.Commit(); err != nil { return err }
type PartWriter struct {
	// dir is the destination directory. Must exist and be
	// writable by the time NewPartWriter is called; we don't
	// mkdir here because the work-dir lifecycle is owned by the
	// orchestrator.
	dir string

	// finalName is the bare filename (no directory, no .part
	// suffix) that Commit renames to.
	finalName string

	// file is the open os.File pointed at dir/finalName.part.
	// Nil after Commit or Abort so the double-close check is
	// cheap.
	file *os.File

	// bytesWritten counts bytes the caller successfully wrote
	// through this writer. Available after Commit via
	// BytesWritten — lets the fetcher cross-check against
	// Content-Length without re-stat'ing the file.
	bytesWritten int64

	// committed tracks whether Commit has run successfully.
	// Abort becomes a no-op after commit; double-commit panics
	// (it'd always be a caller bug).
	committed bool
}

// NewPartWriter creates a new part writer. The .part file is
// created immediately with O_CREATE|O_EXCL to detect stale
// leftovers from a prior crash — a caller that hits os.IsExist
// can delete the stale .part and retry.
//
// Matching the spec's per-segment naming: callers pass the final
// filename (e.g. "42.ts", "init.mp4", "103.m4s") and NewPartWriter
// appends ".part" internally. Keeps the suffix convention in one
// place.
func NewPartWriter(dir, finalName string) (*PartWriter, error) {
	if finalName == "" {
		return nil, fmt.Errorf("hls writer: empty finalName")
	}
	partPath := filepath.Join(dir, finalName+".part")
	// O_EXCL so we fail loudly if a stale .part from a prior
	// job is still on disk. The caller's retry policy can
	// handle cleanup; PartWriter itself refuses to clobber.
	f, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("hls writer open %s: %w", partPath, err)
	}
	return &PartWriter{dir: dir, finalName: finalName, file: f}, nil
}

// Write implements io.Writer. Returns ErrWriterClosed after
// Commit or Abort — protects against late writes from a
// goroutine that didn't notice the writer was already sealed.
func (w *PartWriter) Write(p []byte) (int, error) {
	if w.file == nil {
		return 0, ErrWriterClosed
	}
	n, err := w.file.Write(p)
	w.bytesWritten += int64(n)
	return n, err
}

// BytesWritten returns the running byte count. Valid any time,
// useful both during streaming (progress) and after Commit
// (final size).
func (w *PartWriter) BytesWritten() int64 { return w.bytesWritten }

// FinalPath is the absolute (or dir-relative) path to the final
// file Commit will produce. Useful for logging before Commit
// lands.
func (w *PartWriter) FinalPath() string {
	return filepath.Join(w.dir, w.finalName)
}

// Commit closes the underlying file, fsyncs it, and atomically
// renames .part → final. After a successful Commit, Abort is a
// no-op.
//
// The fsync matters: without it a crash between close and rename
// can leave the filesystem with a zero-length file that looks
// complete to the startup sweep. Paying the sync cost per-segment
// is acceptable — segments are ~100 KB–2 MB, and the segment
// count is bounded by the stream's live window.
func (w *PartWriter) Commit() error {
	if w.committed {
		// Caller bug — double-commit. Log-and-return would mask
		// a concurrent-caller race; panic so the bug surfaces.
		panic("hls writer: double Commit")
	}
	if w.file == nil {
		return ErrWriterClosed
	}
	if err := w.file.Sync(); err != nil {
		_ = w.file.Close()
		w.file = nil
		return fmt.Errorf("hls writer fsync %s: %w", w.finalName, err)
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return fmt.Errorf("hls writer close %s: %w", w.finalName, err)
	}
	w.file = nil
	partPath := filepath.Join(w.dir, w.finalName+".part")
	finalPath := w.FinalPath()
	if err := os.Rename(partPath, finalPath); err != nil {
		return fmt.Errorf("hls writer rename %s → %s: %w", partPath, finalPath, err)
	}
	w.committed = true
	return nil
}

// Abort closes the underlying file and removes the .part file.
// Safe to call multiple times and safe to call after Commit
// (becomes a no-op). Designed to be defer'd at the call site so
// error paths don't need bespoke cleanup.
func (w *PartWriter) Abort() {
	if w.committed {
		return
	}
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	_ = os.Remove(filepath.Join(w.dir, w.finalName+".part"))
}

// ErrWriterClosed is returned by Write / Commit after the writer
// has been sealed via Commit or Abort.
var ErrWriterClosed = fmt.Errorf("hls writer: closed")

// ReadFrom streams from r into the underlying file, avoiding the
// extra buffer allocation io.Copy would do. Implements
// io.ReaderFrom so net/http can fast-path the body copy.
func (w *PartWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.file == nil {
		return 0, ErrWriterClosed
	}
	n, err := io.Copy(w.file, r)
	w.bytesWritten += n
	return n, err
}
