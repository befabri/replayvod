package storage

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// TestErrNotFound_SatisfiesErrNotExist pins the Storage.Stat contract that the
// S3 backend returns an os.ErrNotExist-compatible error. Cross-backend callers
// (e.g. the playback-cache self-heal that drops a ready row whose object is
// gone) rely on errors.Is(err, fs.ErrNotExist) matching on S3 just as it does
// on local storage — without the Is method this silently never fires on S3.
func TestErrNotFound_SatisfiesErrNotExist(t *testing.T) {
	err := error(errNotFound{key: "videos/gone.mp4"})
	if !errors.Is(err, os.ErrNotExist) {
		t.Error("errNotFound does not satisfy errors.Is(_, os.ErrNotExist)")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("errNotFound does not satisfy errors.Is(_, fs.ErrNotExist)")
	}
	if errors.Is(err, os.ErrPermission) {
		t.Error("errNotFound wrongly matches os.ErrPermission")
	}
}

// TestObjectKey_RejectsEscapesAndNormalizes pins the contract the S3
// backend uses: Save("videos/foo.mp4") lands at a safe key without
// leading slashes or path-escape segments. LocalStorage has its own
// resolve() with the same shape — this test covers the S3 path,
// which would otherwise let an attacker controlling the filename
// write outside the configured bucket prefix.
func TestObjectKey_RejectsEscapesAndNormalizes(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
		wantKey string
	}{
		{"normal", "videos/foo.mp4", false, "videos/foo.mp4"},
		{"nested", "videos/2026/foo.mp4", false, "videos/2026/foo.mp4"},
		{"single segment", "foo.mp4", false, "foo.mp4"},
		{"leading slash is stripped", "/videos/foo.mp4", false, "videos/foo.mp4"},
		{"empty", "", true, ""},
		{"root slash", "/", true, ""},
		{"parent ref", "../foo", true, ""},
		{"embedded parent ref", "videos/../etc", true, ""},
		{"embedded dot", "videos/./foo", true, ""},
		{"double slash", "videos//foo", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := objectKey(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("want error for %q, got key=%q", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantKey {
				t.Errorf("got %q, want %q", got, tc.wantKey)
			}
		})
	}
}
