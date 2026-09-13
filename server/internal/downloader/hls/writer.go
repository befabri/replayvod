package hls

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PartWriter publishes complete segments by syncing and renaming temporary files.
// Its zero value is unusable; create one with NewPartWriter and defer Abort.
type PartWriter struct {
	writeFile func(context.Context, *os.File, []byte) (int, error)
	ctx       context.Context

	dir string

	// finalName excludes the directory and temporary .part suffix.
	finalName string

	file *os.File

	bytesWritten int64

	committed bool
}

// NewPartWriter creates a temporary file in an existing writable directory.
// It returns an os.ErrExist-wrapped error for a leftover temporary file; callers own cleanup.
func NewPartWriter(dir, finalName string) (*PartWriter, error) {
	if finalName == "" {
		return nil, fmt.Errorf("hls writer: empty finalName")
	}
	partPath := filepath.Join(dir, finalName+".part")
	f, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("hls writer open %s: %w", partPath, err)
	}
	return &PartWriter{dir: dir, finalName: finalName, file: f}, nil
}

// Write returns ErrWriterClosed after Commit or Abort.
func (w *PartWriter) Write(p []byte) (int, error) {
	if w.file == nil {
		return 0, ErrWriterClosed
	}
	var n int
	var err error
	if w.writeFile != nil {
		n, err = w.writeFile(w.ctx, w.file, p)
	} else {
		n, err = w.file.Write(p)
	}
	w.bytesWritten += int64(n)
	return n, err
}

// BytesWritten returns bytes written since creation or the last Reset, including after Commit.
func (w *PartWriter) BytesWritten() int64 { return w.bytesWritten }

// FinalPath returns the destination path, relative to the directory supplied to NewPartWriter.
func (w *PartWriter) FinalPath() string {
	return filepath.Join(w.dir, w.finalName)
}

// Commit durably publishes the segment and makes Abort a no-op.
// It syncs both file contents and the parent directory; a second successful Commit panics.
func (w *PartWriter) Commit() error {
	if w.committed {
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
		// The renamed file may already be visible; a sync failure cannot confirm its durability.
		return fmt.Errorf("hls writer fsync dir %s: %w", w.dir, err)
	}
	w.committed = true
	return nil
}

// fsyncDir confirms rename durability; some filesystems do not support syncing directories.
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

// Abort removes unfinished output; repeated calls and calls after Commit are harmless.
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

// Reset discards partial bytes without releasing the temporary file.
// It returns ErrWriterClosed after Commit or Abort.
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

// ReadFrom routes io.Copy through Write so chunk accounting and the shared
// scratch budget cannot be bypassed by io.ReaderFrom's optimization.
func (w *PartWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.file == nil {
		return 0, ErrWriterClosed
	}
	return io.Copy(struct{ io.Writer }{w}, r)
}
