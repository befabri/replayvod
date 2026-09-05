package migrations

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
)

// Manifest returns SHA-256 checksums of every SQL file embedded in this binary.
// Paths include the backend (for example postgres/001_users.up.sql). Both up
// and down SQL are included so upgrade and rollback tools can verify their input.
func Manifest() (map[string]string, error) {
	return manifest(all)
}

func manifest(files fs.FS) (map[string]string, error) {
	checksums := make(map[string]string)
	err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		checksums[path] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	return checksums, err
}
