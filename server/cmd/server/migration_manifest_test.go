package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"testing"

	"github.com/befabri/replayvod/server/migrations"
)

func TestWriteMigrationManifest(t *testing.T) {
	var output bytes.Buffer
	if err := writeMigrationManifest(&output); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	expected, err := migrations.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, expected) {
		t.Fatal("diagnostic does not describe embedded runtime migrations")
	}
	if err := writeMigrationManifest(failedManifestWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("lost output error: %v", err)
	}
}

type failedManifestWriter struct{}

func (failedManifestWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
