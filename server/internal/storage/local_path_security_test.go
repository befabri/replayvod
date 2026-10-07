package storage

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalOperationsCannotEscapeThroughSymlinkDirectories(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.mp4")
	if err := os.WriteFile(secret, []byte("outside media"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"escape/secret.mp4", "../" + filepath.Base(outside) + "/secret.mp4", secret} {
		t.Run(key, func(t *testing.T) {
			for name, operation := range map[string]func() error{
				"open": func() error {
					f, err := store.Open(t.Context(), key)
					if f != nil {
						f.Close()
					}
					return err
				},
				"exists": func() error { _, err := store.Exists(t.Context(), key); return err },
				"stat":   func() error { _, err := store.Stat(t.Context(), key); return err },
				"delete": func() error { return store.Delete(t.Context(), key) },
				"save":   func() error { return store.Save(t.Context(), key, strings.NewReader("replaced")) },
			} {
				t.Run(name, func(t *testing.T) {
					if err := operation(); err == nil {
						t.Fatal("storage operation accepted a path outside its root")
					}
				})
			}
		})
	}
	if body, err := os.ReadFile(secret); err != nil || string(body) != "outside media" {
		t.Fatalf("outside media changed: %q, %v", body, err)
	}
}

func TestLocalSymlinkReadsStayInsideRootAndDeletionRemovesOnlyLink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "media.mp4"), []byte("inside media"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.mp4")
	if err := os.WriteFile(secret, []byte("outside media"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"inside-link": "media.mp4", "outside-link": secret} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	f, err := store.Open(t.Context(), "inside-link")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(f)
	f.Close()
	if readErr != nil || string(body) != "inside media" {
		t.Fatalf("contained relative link = %q, %v", body, readErr)
	}
	if f, err := store.Open(t.Context(), "outside-link"); err == nil {
		f.Close()
		t.Fatal("opened an escaping file symlink")
	}
	if _, err := store.Exists(t.Context(), "outside-link"); err == nil {
		t.Fatal("stat followed an escaping file symlink")
	}
	if err := store.Delete(t.Context(), "outside-link"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "outside-link")); !os.IsNotExist(err) {
		t.Fatalf("link was not deleted: %v", err)
	}
	if body, err := os.ReadFile(secret); err != nil || string(body) != "outside media" {
		t.Fatalf("deleting link changed its target: %q, %v", body, err)
	}
}

func TestLocalMissingRootExistsAndDeleteKeepMissingFileSemantics(t *testing.T) {
	store, err := NewLocal(filepath.Join(t.TempDir(), "unmounted"))
	if err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(t.Context(), "videos/missing.mp4"); err != nil || exists {
		t.Fatalf("missing root exists = %v, %v", exists, err)
	}
	if err := store.Delete(t.Context(), "videos/missing.mp4"); err != nil {
		t.Fatalf("missing root delete = %v", err)
	}
}
