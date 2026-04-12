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

// Commit closes the underlying file, fsyncs it, atomically
// renames .part → final, and fsyncs the parent directory so the
// rename itself is durable across a crash. After a successful
// Commit, Abort is a no-op.
//
// The file fsync matters: without it a crash between close and
// rename can leave the filesystem with a zero-length file that
// looks complete to the startup sweep. The directory fsync
// matters for the same reason at the directory-entry level — on
// ext4/XFS the rename isn't durable until the parent dir is
// synced, so a crash between rename and next-dir-sync can leave
// neither the .part nor the final on disk even though Commit
// returned success. Paying both sync costs per-segment is
// acceptable — segments are ~100 KB–2 MB and the segment count
// is bounded by the stream's live window.
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
	if err := fsyncDir(w.dir); err != nil {
		// Rename succeeded but directory-entry durability wasn't
		// confirmed. Don't undo the rename — the file exists;
		// we just can't promise it survives a power loss in the
		// next few ms. Surface the error so operators see it.
		return fmt.Errorf("hls writer fsync dir %s: %w", w.dir, err)
	}
	w.committed = true
	return nil
}

// fsyncDir opens dir and fsyncs it so the most recent rename
// inside it becomes durable. On Linux/macOS os.File.Sync on a
// directory is defined; on Windows the syscall is a no-op and
// the call typically errors — callers can decide whether to
// treat that as fatal or benign.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
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

// Reset truncates the .part file back to zero length and rewinds
// the write offset. Called by the fetch retry loop between
// attempts so that a partial body from a failed 2xx doesn't
// concatenate with the replacement body on the next try.
//
// Returns ErrWriterClosed if called after Commit or Abort.
//
// Reset is not the same as Abort: the file handle stays open and
// the .part file is preserved, so the caller can write fresh
// content without re-racing O_EXCL on a new NewPartWriter.
func (w *PartWriter) Reset() error {
	if w.file == nil {
		return ErrWriterClosed
	}
	if err := w.file.Truncate(0); err != nil {
		return fmt.Errorf("hls writer truncate %s: %w", w.finalName, err)
	}
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("hls writer seek %s: %w", w.finalName, err)
	}
	w.bytesWritten = 0
	return nil
}

// ReadFrom streams from r into the underlying file. Implements
// io.ReaderFrom so callers doing io.Copy(writer, source) pay the
// direct-copy path rather than the generic buffered loop.
//
// Note: this does NOT trigger sendfile/splice for HTTP body →
// file transfers. os.File.ReadFrom only zero-copies when the
// source is itself backed by a poll-able FD (pipe, socket
// exposed via syscall.Conn). HTTP response bodies wrap the
// socket in layers that hide the FD, so io.Copy inside ReadFrom
// falls back to the default 32 KB buffer. The Fetcher drives
// its own pooled-buffer path for segment copies to avoid that
// allocation.
func (w *PartWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.file == nil {
		return 0, ErrWriterClosed
	}
	n, err := io.Copy(w.file, r)
	w.bytesWritten += n
	return n, err
}
