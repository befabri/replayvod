package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const reference = "ghcr.io/befabri/replayvod:2.8.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func entry(os, arch, digest string) string {
	return fmt.Sprintf(`{"digest":%q,"platform":{"os":%q,"architecture":%q}}`, digest, os, arch)
}

func TestPlatformImagesPinEachArchitecture(t *testing.T) {
	amd, arm := "sha256:"+strings.Repeat("b", 64), "sha256:"+strings.Repeat("c", 64)
	index := `{"manifests":[` + entry("linux", "amd64", amd) + "," + entry("unknown", "unknown", "sha256:"+strings.Repeat("d", 64)) + "," + entry("linux", "arm64", arm) + `]}`
	images, err := platformImages([]byte(index), reference)
	if err != nil {
		t.Fatal(err)
	}
	name := "ghcr.io/befabri/replayvod:2.8.0@"
	if len(images) != 2 || images["amd64"] != name+amd || images["arm64"] != name+arm {
		t.Fatalf("images = %v", images)
	}
	for name, manifests := range map[string]string{
		"missing arm64":         entry("linux", "amd64", amd),
		"duplicate arm64":       entry("linux", "amd64", amd) + "," + entry("linux", "arm64", arm) + "," + entry("linux", "arm64", amd),
		"tag instead of digest": entry("linux", "amd64", "latest") + "," + entry("linux", "arm64", arm),
		"single image manifest": "",
	} {
		if _, err := platformImages([]byte(`{"manifests":[`+manifests+`]}`), reference); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestManifestRoundTripsUnchanged(t *testing.T) {
	path := "../../tests/upgrade/testdata/manifest.json"
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(data, '\n'), original) {
		t.Fatal("manifest.json is not in the form this tool writes; rewrite it with writeManifest")
	}
}
