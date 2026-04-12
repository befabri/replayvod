package storage

import "testing"

// TestObjectKey_RejectsEscapesAndNormalizes pins the contract the S3
// backend uses: Save("videos/foo.mp4") lands at a safe key without
// leading slashes or path-escape segments. LocalStorage has its own
// resolve() with the same shape — this test covers the S3 path,
// which would otherwise let an attacker controlling the filename
// write outside the configured bucket prefix.
func TestObjectKey_RejectsEscapesAndNormalizes(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantErr   bool
		wantKey   string
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
