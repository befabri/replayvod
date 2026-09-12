package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type failingWriteProbe struct {
	*LocalStorage
	err error
}

func (s failingWriteProbe) ProbeWrite(context.Context) error {
	return &os.PathError{Op: "write", Path: s.Root, Err: s.err}
}

func TestWriteProbeClassifiesCapacityPermissionsAndOutages(t *testing.T) {
	local, _ := newLocalRoot(t)
	attached, err := Attach(t.Context(), local, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cause, want error
		read, del   bool
	}{
		{syscall.ENOSPC, ErrFull, true, true},
		{syscall.EDQUOT, ErrFull, true, true},
		{syscall.EROFS, ErrReadOnly, true, false},
		{syscall.EACCES, ErrReadOnly, true, false},
		{syscall.EPERM, ErrReadOnly, true, false},
		{syscall.EIO, ErrUnreachable, false, false},
		{syscall.ENOENT, ErrUnreachable, false, false},
		{context.DeadlineExceeded, ErrUnreachable, false, false},
	} {
		t.Run(tc.cause.Error(), func(t *testing.T) {
			err := Ready(t.Context(), failingWriteProbe{local, tc.cause}, attached.ID)
			if !errors.Is(err, tc.want) || !errors.Is(err, tc.cause) {
				t.Fatalf("Ready = %v; want %v wrapping %v", err, tc.want, tc.cause)
			}
			if CanRead(err) != tc.read || CanDelete(err) != tc.del {
				t.Fatalf("verdict %v: read=%v delete=%v", err, CanRead(err), CanDelete(err))
			}
		})
	}
}

func TestCapacityNeverAuthenticatesForeignStorage(t *testing.T) {
	local, _ := newLocalRoot(t)
	if _, err := Attach(t.Context(), local, ""); err != nil {
		t.Fatal(err)
	}
	err := Ready(t.Context(), failingWriteProbe{local, syscall.ENOSPC}, strings.Repeat("f", 64))
	if !errors.Is(err, ErrUnattached) || CanRead(err) || CanDelete(err) {
		t.Fatalf("full foreign storage trusted: %v", err)
	}
}

func TestMediaSaveNeverRecreatesMissingRoot(t *testing.T) {
	local, root := newLocalRoot(t)
	err := local.Save(t.Context(), "videos/recording.mp4", strings.NewReader("recording"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("save on missing root = %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("save recreated the root: %v", err)
	}
}

type replaceRootReader struct {
	*strings.Reader
	replace func()
}

func (r *replaceRootReader) Read(p []byte) (int, error) {
	if r.replace != nil {
		r.replace()
		r.replace = nil
	}
	return r.Reader.Read(p)
}

func TestLocalSavePinsRootAcrossReplacement(t *testing.T) {
	local, root := newLocalRoot(t)
	if _, err := Attach(t.Context(), local, ""); err != nil {
		t.Fatal(err)
	}
	detached := root + "-detached"
	const key = "videos/recording.mp4"
	r := &replaceRootReader{Reader: strings.NewReader("trusted recording"), replace: func() {
		if err := os.Rename(root, detached); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "videos"), 0o755); err != nil {
			t.Fatal(err)
		}
		// A replacement may contain the same deterministic temporary key.
		if err := os.WriteFile(filepath.Join(root, key+".tmp"), []byte("foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	if err := local.Save(t.Context(), key, r); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(detached, key)); err != nil || string(got) != "trusted recording" {
		t.Fatalf("original root lost upload: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, key+".tmp")); err != nil || string(got) != "foreign" {
		t.Fatalf("upload touched replacement root: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, key)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("upload published on replacement root: %v", err)
	}
}
