package remux

import (
	"context"
	"errors"
	"io"
	"os"
)

// FileOperations coordinates scratch mutations with their capacity accounting.
// Nil operations use local files without workspace accounting.
type FileOperations interface {
	WriteFile(context.Context, *os.File, []byte) (int, error)
	Rename(string, string) error
	Remove(string) error
	Truncate(*os.File, int64) error
}

type localFiles struct{}

func (localFiles) WriteFile(ctx context.Context, file *os.File, data []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return file.Write(data)
}

func (localFiles) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (localFiles) Remove(path string) error             { return os.Remove(path) }
func (localFiles) Truncate(file *os.File, size int64) error {
	return file.Truncate(size)
}

func filesOrLocal(files FileOperations) FileOperations {
	if files == nil {
		return localFiles{}
	}
	return files
}

// removePartial prevents ffmpeg's -y truncation from bypassing scratch accounting.
func removePartial(files FileOperations, path string) error {
	err := files.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func writeInputFile(ctx context.Context, path string, data []byte, files FileOperations) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	files = filesOrLocal(files)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	// Truncation must release the previous input's accounted bytes before replacement.
	if err := files.Truncate(file, 0); err != nil {
		return err
	}
	n, err := files.WriteFile(ctx, file, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}
