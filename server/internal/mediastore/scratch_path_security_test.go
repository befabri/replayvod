package mediastore

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestScratchOperationsCannotFollowDirectoryLinksOutsideWorkspace(t *testing.T) {
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
			for name, operate := range map[string]func() error{
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
				"open": func() error {
					f, err := workspace.Open(escapingPath)
					if f != nil {
						f.Close()
					}
					return err
				},
			} {
				t.Run(name, func(t *testing.T) {
					if err := operate(); err == nil {
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

func TestScratchFileLinksCannotRedirectWorkspaceFiles(t *testing.T) {
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
	if f, err := workspace.Open(link); err == nil {
		f.Close()
		t.Fatal("open followed an escaping file link")
	}
	if f, err := workspace.create(link); err == nil {
		f.Close()
		t.Fatal("create followed an escaping file link")
	}
	if err := workspace.Remove(link); err != nil {
		t.Fatalf("removing the link itself: %v", err)
	}
	if body, err := os.ReadFile(external); err != nil || string(body) != "outside media" {
		t.Fatalf("external media changed: %q, %v", body, err)
	}
}

func TestScratchOpenReadsWorkspaceFiles(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	workspace, err := scratch.New("capture", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close(true)
	path := filepath.Join(workspace.Dir, "segments", "part.mp4")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("captured media"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(body) != "captured media" {
		t.Fatalf("workspace read = %q, %v", body, err)
	}
	if err := workspace.Close(false); err != nil {
		t.Fatal(err)
	}
	if f, err := workspace.Open(path); err == nil {
		f.Close()
		t.Fatal("closed workspace still read recovery files")
	}
}

func TestScratchRefusesWorkspaceReachedThroughLink(t *testing.T) {
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
	if workspace, err := scratch.Open(recoveryDir, 0); err == nil {
		workspace.Close(false)
		t.Fatal("recovery directory link was opened as a workspace")
	}
	if len(scratch.work) != 1 {
		t.Fatalf("refused workspace kept a reservation: %d owners", len(scratch.work))
	}
	if body, err := os.ReadFile(file); err != nil || string(body) != "peer media" {
		t.Fatalf("peer media changed: %q, %v", body, err)
	}
}
