package downloader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errInvalidRecordingPath = errors.New("invalid recording path")

// Recording names are single path components, including names restored from
// checkpoints. Never let external broadcaster metadata choose a directory.
func validateRecordingName(name string) error {
	if !filepath.IsLocal(name) || filepath.Base(name) != name || name == "." || strings.ContainsAny(name, "/\\\x00\r\n") {
		return fmt.Errorf("%w: recording name must be a single filename", errInvalidRecordingPath)
	}
	return nil
}

// openScratch reads only this attempt's files. Root-scoped opens also reject
// symlinks that leave scratch or enter another attempt through an outside path.
func (s *Service) openScratch(d *download, path string) (*os.File, error) {
	if err := validateRecordingName(d.jobID); err != nil {
		return nil, err
	}
	jobDir, err := filepath.Abs(filepath.Join(s.cfg.Env.ScratchDir, d.jobID))
	if err != nil {
		return nil, fmt.Errorf("%w: resolve job scratch: %w", errInvalidRecordingPath, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve scratch file: %w", errInvalidRecordingPath, err)
	}
	rel, err := filepath.Rel(jobDir, abs)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("%w: file must be inside its recording scratch directory", errInvalidRecordingPath)
	}
	root, err := os.OpenRoot(s.cfg.Env.ScratchDir)
	if err != nil {
		return nil, fmt.Errorf("open scratch root: %w", err)
	}
	defer root.Close()
	info, err := root.Lstat(d.jobID)
	if err != nil {
		return nil, fmt.Errorf("%w: stat recording scratch: %w", errInvalidRecordingPath, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: recording scratch must be a directory", errInvalidRecordingPath)
	}
	job, err := openScratchJob(root, d.jobID, info)
	if err != nil {
		return nil, err
	}
	defer job.Close()
	file, err := job.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("%w: open recording file: %w", errInvalidRecordingPath, err)
	}
	return file, nil
}

// Match the opened directory to the entry checked before opening. A swapped
// symlink can stay within scratch while pointing at another recording's files.
func openScratchJob(root *os.Root, jobID string, expected os.FileInfo) (*os.Root, error) {
	job, err := root.OpenRoot(jobID)
	if err != nil {
		return nil, fmt.Errorf("%w: open recording scratch: %w", errInvalidRecordingPath, err)
	}
	opened, err := job.Stat(".")
	if err != nil {
		job.Close()
		return nil, fmt.Errorf("%w: stat opened recording scratch: %w", errInvalidRecordingPath, err)
	}
	if !os.SameFile(expected, opened) {
		job.Close()
		return nil, fmt.Errorf("%w: recording scratch changed while opening", errInvalidRecordingPath)
	}
	return job, nil
}
