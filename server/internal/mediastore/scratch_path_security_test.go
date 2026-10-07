package mediastore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScratchMutationsCannotFollowDirectorySymlinksOutsideWorkspace(t *testing.T) {
	for _, targetLocation := range []string{"outside scratch", "peer workspace"} {
		t.Run(targetLocation, func(t *testing.T) {
			scratch := NewScratch(t.TempDir())
			workspace, err := scratch.New("capture", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close(true)
			outside := t.TempDir()
			if targetLocation == "peer workspace" {
				peer, err := scratch.New("peer", 0)
				if err != nil {
					t.Fatal(err)
				}
				defer peer.Close(true)
				outside = peer.Dir
			}
			external := filepath.Join(outside, "media.mp4")
			if err := os.WriteFile(external, []byte("outside media"), 0600); err != nil {
				t.Fatal(err)
			}
			inside := filepath.Join(workspace.Dir, "inside.mp4")
			if err := os.WriteFile(inside, []byte("inside media"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(workspace.Dir, "escape")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			escapingPath := filepath.Join(link, "media.mp4")
			for name, mutate := range map[string]func() error{
				"rename from link": func() error { return workspace.Rename(escapingPath, inside) },
				"rename to link":   func() error { return workspace.Rename(inside, escapingPath) },
				"remove":           func() error { return workspace.Remove(escapingPath) },
				"create": func() error {
					f, err := workspace.create(escapingPath)
					if f != nil {
						f.Close()
					}
					return err
				},
			} {
				t.Run(name, func(t *testing.T) {
					if err := mutate(); err == nil {
						t.Fatal("scratch operation followed an escaping directory link")
					}
				})
			}
			if body, err := os.ReadFile(external); err != nil || string(body) != "outside media" {
				t.Fatalf("external media changed: %q, %v", body, err)
			}
			if body, err := os.ReadFile(inside); err != nil || string(body) != "inside media" {
				t.Fatalf("workspace media changed: %q, %v", body, err)
			}
		})
	}
}

func TestScratchRejectsCallerOpenedEscapingSymlinkHandles(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	workspace, err := scratch.New("capture", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close(true)
	external := filepath.Join(t.TempDir(), "media.mp4")
	if err := os.WriteFile(external, []byte("outside media"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace.Dir, "media.mp4")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(link, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := workspace.Truncate(file, 0); err == nil {
		t.Fatal("truncate accepted an escaping symlink handle")
	}
	if _, err := workspace.WriteFile(t.Context(), file, []byte("changed")); err == nil {
		t.Fatal("write accepted an escaping symlink handle")
	}
	if f, err := workspace.create(link); err == nil {
		f.Close()
		t.Fatal("create followed an escaping file link")
	}
	if err := workspace.Remove(link); err != nil {
		t.Fatalf("removing link itself: %v", err)
	}
	if body, err := os.ReadFile(external); err != nil || string(body) != "outside media" {
		t.Fatalf("external media changed: %q, %v", body, err)
	}
}

func TestScratchRecoveryDirectorySymlinkCannotMutateAnotherWorkspace(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	peer, err := scratch.New("peer", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(true)
	file := filepath.Join(peer.Dir, "media.mp4")
	if err := os.WriteFile(file, []byte("peer media"), 0600); err != nil {
		t.Fatal(err)
	}
	recoveryDir := filepath.Join(scratch.Root(), "recovery")
	if err := os.Symlink(filepath.Base(peer.Dir), recoveryDir); err != nil {
		t.Fatal(err)
	}
	workspace, err := scratch.Open(recoveryDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close(true)
	if opened, err := workspace.create(filepath.Join(recoveryDir, "media.mp4")); err == nil {
		opened.Close()
		t.Fatal("recovery directory link redirected mutation to peer workspace")
	}
	if body, err := os.ReadFile(file); err != nil || string(body) != "peer media" {
		t.Fatalf("peer media changed: %q, %v", body, err)
	}
}
