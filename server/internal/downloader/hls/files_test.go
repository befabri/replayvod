package hls

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/replayvod/server/internal/mediastore"
)

type writeFileFunc func(context.Context, *os.File, []byte) (int, error)

func (f writeFileFunc) WriteFile(ctx context.Context, file *os.File, p []byte) (int, error) {
	return f(ctx, file, p)
}

func (writeFileFunc) Rename(oldPath, newPath string) error     { return os.Rename(oldPath, newPath) }
func (writeFileFunc) Remove(path string) error                 { return os.Remove(path) }
func (writeFileFunc) Truncate(file *os.File, size int64) error { return file.Truncate(size) }

func TestPartWriterCannotMutateReleasedWorkspace(t *testing.T) {
	for _, operation := range []string{"commit", "reset", "abort"} {
		t.Run(operation, func(t *testing.T) {
			scratch := mediastore.NewScratch(t.TempDir())
			workspace, err := scratch.New("capture", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close(true)
			writer, err := NewPartWriter(workspace.Dir, "segment.ts")
			if err != nil {
				t.Fatal(err)
			}
			writer.ctx, writer.files = t.Context(), workspace
			defer writer.Abort()
			const captured = "saved segment bytes"
			if _, err := writer.Write([]byte(captured)); err != nil {
				t.Fatal(err)
			}
			if err := workspace.Close(false); err != nil {
				t.Fatal(err)
			}
			cleanup, err := scratch.OpenForCleanup(workspace.Dir)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup.Close(true)
			switch operation {
			case "commit":
				if err := writer.Commit(); err == nil {
					t.Fatal("released writer published another owner's segment")
				}
			case "reset":
				if err := writer.Reset(); err == nil {
					t.Fatal("released writer truncated another owner's segment")
				}
			case "abort":
				writer.Abort()
			}
			partial := filepath.Join(workspace.Dir, "segment.ts.part")
			if body, err := os.ReadFile(partial); err != nil || string(body) != captured {
				t.Fatalf("released %s changed recovery input: %q, %v", operation, body, err)
			}
			if _, err := os.Stat(writer.FinalPath()); !os.IsNotExist(err) {
				t.Fatalf("released %s published output: %v", operation, err)
			}
		})
	}
}
