package migrations

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestManifestHashesContentAndIncludesBothDirections(t *testing.T) {
	files := fstest.MapFS{
		"postgres/001.up.sql":   &fstest.MapFile{Data: []byte("abc")},
		"postgres/001.down.sql": &fstest.MapFile{Data: []byte("")},
		"sqlite/001.up.sql":     &fstest.MapFile{Data: []byte("abc")},
	}
	got, err := manifest(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["postgres/001.up.sql"] != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || got["postgres/001.down.sql"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" || got["sqlite/001.up.sql"] != got["postgres/001.up.sql"] {
		t.Fatalf("unexpected checksums: %v", got)
	}
}

func TestManifestCoversRuntimeMigrations(t *testing.T) {
	got, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for backend, files := range map[string]fs.FS{"postgres": Postgres(), "sqlite": SQLite()} {
		paths, err := fs.Glob(files, "*.sql")
		if err != nil || len(paths) == 0 {
			t.Fatalf("%s files: %v, %v", backend, paths, err)
		}
		for _, path := range paths {
			if len(got[backend+"/"+path]) != 64 {
				t.Fatalf("missing checksum for %s/%s", backend, path)
			}
			count++
		}
	}
	if len(got) != count {
		t.Fatalf("manifest contains %d files; runtime files: %d", len(got), count)
	}
}
