// Command publish-images pushes the tested per-architecture images to the
// release repository and assembles their multi-platform tags. Every archive is
// validated before the first registry write, and manifests are pushed by digest
// so no intermediate tag ever appears on the package.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/v1/validate"
)

var architectures = []string{"amd64", "arm64"}

// ociLabels are required on every published image.
var ociLabels = []string{"created", "title", "description", "licenses", "url", "source", "revision", "version"}

func main() {
	artifacts := os.Getenv("UPGRADE_IMAGES")
	if artifacts == "" {
		artifacts = "/tmp/upgrade-images"
	}
	var tags []string
	for _, tag := range strings.Split(os.Getenv("RELEASE_TAGS"), "\n") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	index, err := publish(context.Background(), artifacts, strings.ToLower(os.Getenv("RELEASE_IMAGE")), os.Getenv("GITHUB_SHA"), tags, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(index)
}

// publish validates every archive, then pushes each image by digest and tags a
// manifest list referencing them. It returns the published index reference.
func publish(ctx context.Context, artifacts, image, revision string, tags []string, opts ...remote.Option) (string, error) {
	if !regexp.MustCompile(`^[a-z0-9.:/_-]+$`).MatchString(image) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(revision) {
		return "", errors.New("invalid image repository or release revision")
	}
	if len(tags) == 0 {
		return "", errors.New("release tags must be nonempty and unique")
	}
	seen := map[string]bool{}
	tag := regexp.MustCompile(`^` + regexp.QuoteMeta(image) + `:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	for _, t := range tags {
		if seen[t] {
			return "", errors.New("release tags must be nonempty and unique")
		}
		if !tag.MatchString(t) {
			return "", fmt.Errorf("release tag does not belong to %s: %s", image, t)
		}
		seen[t] = true
	}
	repository, err := name.NewRepository(image)
	if err != nil {
		return "", err
	}
	var images []v1.Image
	var addenda []mutate.IndexAddendum
	for _, arch := range architectures {
		expected, err := os.ReadFile(filepath.Join(artifacts, arch+".id"))
		if err != nil {
			return "", err
		}
		img, err := inspectArchive(filepath.Join(artifacts, arch+".tar"), strings.TrimSpace(string(expected)), arch, revision)
		if err != nil {
			return "", err
		}
		images = append(images, img)
		addenda = append(addenda, mutate.IndexAddendum{Add: img, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: arch}}})
	}
	opts = append(opts, remote.WithContext(ctx))
	for _, img := range images {
		digest, err := img.Digest()
		if err != nil {
			return "", err
		}
		if err := remote.Write(repository.Digest(digest.String()), img, opts...); err != nil {
			return "", err
		}
	}
	index := mutate.IndexMediaType(mutate.AppendManifests(empty.Index, addenda...), types.DockerManifestList)
	for i, t := range tags {
		ref, err := name.NewTag(t)
		if err != nil {
			return "", err
		}
		if i == 0 {
			err = remote.WriteIndex(ref, index, opts...)
		} else {
			err = remote.Tag(ref, index, opts...)
		}
		if err != nil {
			return "", err
		}
	}
	digest, err := index.Digest()
	if err != nil {
		return "", err
	}
	return image + "@" + digest.String(), nil
}

// inspectArchive loads the tested image for arch and rejects anything but the
// image the upgrade job saved.
func inspectArchive(path, expectedID, arch, revision string) (v1.Image, error) {
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: expected exactly the tested %s image: %w", path, arch, err)
	}
	id, err := img.ConfigName()
	if err != nil {
		return nil, err
	}
	if id.String() != expectedID {
		return nil, fmt.Errorf("%s: image ID differs from the tested image", path)
	}
	config, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	if config.OS != "linux" || config.Architecture != arch {
		return nil, fmt.Errorf("%s: unexpected image platform %s/%s", path, config.OS, config.Architecture)
	}
	labels := config.Config.Labels
	if labels["org.opencontainers.image.revision"] != revision {
		return nil, fmt.Errorf("%s: source revision differs from the release commit", path)
	}
	for _, label := range ociLabels {
		if _, ok := labels["org.opencontainers.image."+label]; !ok {
			return nil, fmt.Errorf("%s: missing OCI label %s", path, label)
		}
	}
	if err := validate.Image(img); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}
