package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newLocalRoot(t *testing.T) (*LocalStorage, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "storage")
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	return store, root
}

func readMarkerFile(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, MarkerPath))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func TestNewLocalDoesNotCreateTheRoot(t *testing.T) {
	_, root := newLocalRoot(t)
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("root stat = %v, want not exist", err)
	}
}

func TestAttachFirstBootInitializesMarkerAndRoot(t *testing.T) {
	store, root := newLocalRoot(t)
	res, err := Attach(t.Context(), store, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != AttachInitialized || !validStorageID(res.ID) {
		t.Fatalf("result = %+v", res)
	}
	if got := readMarkerFile(t, root); got != res.ID {
		t.Fatalf("marker = %q, want %q", got, res.ID)
	}
	if err := Ready(t.Context(), store, res.ID); err != nil {
		t.Fatalf("ready after init = %v", err)
	}
}

func TestAttachAdoptsExistingMarkerWhenDatabaseHasNone(t *testing.T) {
	store, root := newLocalRoot(t)
	id, _ := NewStorageID()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, MarkerPath), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Attach(t.Context(), store, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != AttachAdopted || res.ID != id {
		t.Fatalf("result = %+v, want adopted %s", res, id)
	}
}

func TestAttachMatchesRecordedID(t *testing.T) {
	store, _ := newLocalRoot(t)
	first, err := Attach(t.Context(), store, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Attach(t.Context(), store, first.ID)
	if err != nil || res.Outcome != AttachMatched || res.ID != first.ID {
		t.Fatalf("result = %+v, %v", res, err)
	}
}

func TestReadyRefusesMissingRootWithoutCreatingIt(t *testing.T) {
	store, root := newLocalRoot(t)
	id, _ := NewStorageID()
	err := Ready(t.Context(), store, id)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("ready = %v, want ErrUnreachable", err)
	}
	if _, statErr := os.Stat(root); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatal("a readiness check created the root")
	}
	if _, err := Attach(t.Context(), store, id); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("attach with a recorded id = %v, want ErrUnreachable", err)
	}
}

func TestReadyDistinguishesMissingMalformedAndForeignMarkers(t *testing.T) {
	store, root := newLocalRoot(t)
	expected, _ := NewStorageID()
	other, _ := NewStorageID()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		marker *string
		want   string
	}{
		{name: "missing", marker: nil, want: "missing"},
		{name: "malformed", marker: ptr("not-an-id"), want: "malformed"},
		{name: "empty", marker: ptr(""), want: "malformed"},
		{name: "oversized", marker: ptr(expected + strings.Repeat(" ", 64) + "unexpected content"), want: "malformed"},
		{name: "foreign", marker: &other, want: "different install"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, MarkerPath)
			_ = os.Remove(path)
			if tc.marker != nil {
				if err := os.WriteFile(path, []byte(*tc.marker), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := Ready(t.Context(), store, expected)
			if !errors.Is(err, ErrUnattached) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ready = %v, want ErrUnattached mentioning %q", err, tc.want)
			}
			if _, err := Attach(t.Context(), store, expected); !errors.Is(err, ErrUnattached) {
				t.Fatalf("attach = %v, want ErrUnattached", err)
			}
		})
	}
}

func TestReadyRequiresARecordedID(t *testing.T) {
	store, _ := newLocalRoot(t)
	if _, err := Attach(t.Context(), store, ""); err != nil {
		t.Fatal(err)
	}
	if err := Ready(t.Context(), store, ""); !errors.Is(err, ErrUnattached) {
		t.Fatalf("ready = %v, want ErrUnattached", err)
	}
}

func TestReadyReportsReadOnlyRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	store, root := newLocalRoot(t)
	res, err := Attach(t.Context(), store, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if err := Ready(t.Context(), store, res.ID); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("ready = %v, want ErrReadOnly", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ready(t.Context(), store, res.ID); err != nil {
		t.Fatalf("ready after remount = %v", err)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".replayvod-write-probe") {
			t.Fatalf("write probe left %s behind", e.Name())
		}
	}
}

func TestWriteMarkerClaimsForeignStorage(t *testing.T) {
	store, root := newLocalRoot(t)
	mine, _ := NewStorageID()
	theirs, _ := NewStorageID()
	if err := WriteMarker(t.Context(), store, theirs); err != nil {
		t.Fatal(err)
	}
	if err := Ready(t.Context(), store, mine); !errors.Is(err, ErrUnattached) {
		t.Fatalf("ready before adopt = %v", err)
	}
	if err := WriteMarker(t.Context(), store, mine); err != nil {
		t.Fatal(err)
	}
	if err := Ready(t.Context(), store, mine); err != nil {
		t.Fatalf("ready after adopt = %v", err)
	}
	if got := readMarkerFile(t, root); got != mine {
		t.Fatalf("marker = %q, want %q", got, mine)
	}
	if err := WriteMarker(t.Context(), store, "bogus"); err == nil {
		t.Fatal("accepted an invalid id")
	}
}

func TestReadyHonorsCancellation(t *testing.T) {
	store, _ := newLocalRoot(t)
	res, err := Attach(t.Context(), store, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Ready(ctx, store, res.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("ready = %v, want context.Canceled", err)
	}
}

func ptr(s string) *string { return &s }
