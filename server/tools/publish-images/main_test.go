package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/v1/validate"
)

var revision = strings.Repeat("a", 40)

type archive struct {
	revision, platform, missingLabel string
	payload                          []byte
}

func fixtureLabels(rev string) map[string]string {
	labels := map[string]string{}
	for _, label := range ociLabels {
		labels["org.opencontainers.image."+label] = "fixture"
	}
	labels["org.opencontainers.image.revision"] = rev
	return labels
}

// writeArchive saves a synthetic tested image as the upgrade job would and
// returns its image ID and labels.
func writeArchive(t *testing.T, dir, arch string, a archive) (string, map[string]string) {
	t.Helper()
	if a.revision == "" {
		a.revision = revision
	}
	if a.platform == "" {
		a.platform = arch
	}
	if a.payload == nil {
		a.payload = []byte("recording fixture")
	}
	labels := fixtureLabels(a.revision)
	delete(labels, "org.opencontainers.image."+a.missingLabel)
	img, err := mutate.AppendLayers(empty.Image, static.NewLayer(tarOf(a.payload), types.DockerUncompressedLayer))
	if err != nil {
		t.Fatal(err)
	}
	config, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	config = config.DeepCopy()
	config.OS, config.Architecture, config.Config.Labels = "linux", a.platform, labels
	if img, err = mutate.ConfigFile(img, config); err != nil {
		t.Fatal(err)
	}
	if err := tarball.WriteToFile(filepath.Join(dir, arch+".tar"), parseReference(t, "replayvod:upgrade-"+arch), img); err != nil {
		t.Fatal(err)
	}
	id, err := img.ConfigName()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, arch+".id"), []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return id.String(), labels
}

func parseReference(t *testing.T, s string) name.Reference {
	t.Helper()
	ref, err := name.ParseReference(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// tarOf wraps payload in an uncompressed tar, the layer format docker save emits.
func tarOf(payload []byte) []byte {
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	if err := writer.WriteHeader(&tar.Header{Name: "fixture", Size: int64(len(payload)), Mode: 0o644}); err != nil {
		panic(err)
	}
	if _, err := writer.Write(payload); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func newRegistry(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://")
}

func TestPublishAssemblesTestedImagesWithoutIntermediateTags(t *testing.T) {
	ctx := context.Background()
	image := newRegistry(t) + "/release"
	tags := []string{image + ":2.8.0", image + ":2.8", image + ":latest"}
	dir := t.TempDir()
	expected := map[string]string{}
	labels := map[string]map[string]string{}
	for _, arch := range architectures {
		expected[arch], labels[arch] = writeArchive(t, dir, arch, archive{})
	}
	index, err := publish(ctx, dir, image, revision, tags)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := publish(ctx, dir, image, revision, tags); err != nil || again != index {
		t.Fatalf("repeated publication produced %s, %v; want %s", again, err, index)
	}
	repository, err := name.NewRepository(image)
	if err != nil {
		t.Fatal(err)
	}
	published, err := remote.List(repository)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(published, ",") != "2.8,2.8.0,latest" {
		t.Fatalf("published tags: %v", published)
	}
	digest := strings.TrimPrefix(index, image+"@")
	for _, tag := range tags {
		head, err := remote.Head(parseReference(t, tag))
		if err != nil || head.Digest.String() != digest {
			t.Fatalf("%s points at %v, %v; want %s", tag, head, err, digest)
		}
	}
	list, err := remote.Index(parseReference(t, tags[0]))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := list.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.MediaType != types.DockerManifestList || len(manifest.Manifests) != len(architectures) {
		t.Fatalf("index manifest: %+v", manifest)
	}
	for _, desc := range manifest.Manifests {
		arch := desc.Platform.Architecture
		if desc.Platform.OS != "linux" || expected[arch] == "" {
			t.Fatalf("unexpected platform %+v", desc.Platform)
		}
		img, err := remote.Image(repository.Digest(desc.Digest.String()))
		if err != nil {
			t.Fatal(err)
		}
		id, err := img.ConfigName()
		if err != nil {
			t.Fatal(err)
		}
		if id.String() != expected[arch] {
			t.Fatalf("%s image ID changed: %s, want %s", arch, id, expected[arch])
		}
		config, err := img.ConfigFile()
		if err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(config.Config.Labels, labels[arch]) {
			t.Fatalf("%s labels changed: %v", arch, config.Config.Labels)
		}
		if err := validate.Image(img); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublishRejectsIncompleteArchivesBeforeAnyWrite(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		arm64 *archive
	}{
		{"missing archive", nil},
		{"foreign revision", &archive{revision: strings.Repeat("b", 40)}},
		{"wrong platform", &archive{platform: "amd64"}},
		{"missing label", &archive{missingLabel: "licenses"}},
		{"corrupt layer", &archive{payload: []byte("corrupted fixture")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := newRegistry(t)
			image := host + "/rejected"
			dir := t.TempDir()
			writeArchive(t, dir, "amd64", archive{})
			if tc.arm64 != nil {
				writeArchive(t, dir, "arm64", *tc.arm64)
			}
			if tc.name == "corrupt layer" {
				writeArchive(t, dir, "arm64", archive{})
				corruptLayer(t, filepath.Join(dir, "arm64.tar"))
			}
			if _, err := publish(ctx, dir, image, revision, []string{image + ":latest"}); err == nil {
				t.Fatal("incomplete publication was accepted")
			}
			reg, err := name.NewRegistry(host)
			if err != nil {
				t.Fatal(err)
			}
			repositories, err := remote.Catalog(ctx, reg)
			if err != nil || len(repositories) != 0 {
				t.Fatalf("registry was written before validation completed: %v, %v", repositories, err)
			}
		})
	}
}

func TestPublishValidatesTagsBeforeReadingArchives(t *testing.T) {
	image := "localhost:5000/replayvod"
	for _, tags := range [][]string{nil, {image + ":latest", image + ":latest"}, {"elsewhere/image:latest"}, {image + ":latest\nother"}} {
		if _, err := publish(context.Background(), "missing", image, revision, tags); err == nil || strings.Contains(err.Error(), "missing") {
			t.Fatalf("tags %v: %v", tags, err)
		}
	}
}

// corruptLayer rewrites the archive with the first layer's bytes altered so
// its digest no longer matches the manifest.
func corruptLayer(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	var order []string
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = content
		order = append(order, header.Name)
	}
	var manifest []struct{ Layers []string }
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	layer := manifest[0].Layers[0]
	entries[layer] = bytes.Replace(entries[layer], []byte("recording"), []byte("corrupted"), 1)
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	for _, name := range order {
		if err := writer.WriteHeader(&tar.Header{Name: name, Size: int64(len(entries[name])), Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entries[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
