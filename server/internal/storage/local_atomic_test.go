package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

func TestLocalSaveOverlappingWritersPublishWholeFiles(t *testing.T) {
	store, root := newLocalRoot(t)
	if _, err := Attach(t.Context(), store, ""); err != nil {
		t.Fatal(err)
	}
	const key = "videos/recording.mp4"
	const first = "first recording"
	const second = "second recording is longer"
	// Complete another writer while this writer has its temporary file open.
	// Readers must see that complete version until this writer publishes.
	r := readFunc(func(p []byte) (int, error) {
		if err := store.Save(t.Context(), key, strings.NewReader(second)); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(root, key)); err != nil || string(got) != second {
			t.Fatalf("overlapping save = %q, %v", got, err)
		}
		return copy(p, first), io.EOF
	})
	if err := store.Save(t.Context(), key, r); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, key)); err != nil || string(got) != first {
		t.Fatalf("final recording = %q, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "videos"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "recording.mp4" {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func TestLocalSaveCanceledFinalReadPreservesPublishedFile(t *testing.T) {
	store, root := newLocalRoot(t)
	if _, err := Attach(t.Context(), store, ""); err != nil {
		t.Fatal(err)
	}
	const key = "videos/recording.mp4"
	if err := store.Save(t.Context(), key, strings.NewReader("original")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := readFunc(func(p []byte) (int, error) {
		cancel()
		return copy(p, "canceled replacement"), io.EOF
	})
	if err := store.Save(ctx, key, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, key)); err != nil || string(got) != "original" {
		t.Fatalf("canceled save replaced published file: %q, %v", got, err)
	}
}
