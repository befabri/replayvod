package contracttest

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testMediaPublicationLifecycle(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	if _, err := repo.GetMediaPublication(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing publication: %v", err)
	}
	if err := repo.ConfirmMediaPublication(ctx, "missing", "digest"); err != nil {
		t.Fatalf("confirming a missing publication: %v", err)
	}
	if err := repo.RequestMediaPublicationDelete(ctx, "missing"); err != nil {
		t.Fatalf("requesting deletion of a missing publication: %v", err)
	}
	if err := repo.DeleteMediaPublication(ctx, "missing"); err != nil {
		t.Fatalf("deleting a missing publication: %v", err)
	}
	input := repository.MediaPublication{Key: "videos/1/poster.jpg", VideoID: 1, Digest: "aaa", SizeBytes: 10}
	begun, err := repo.BeginMediaPublication(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	want := input
	want.Unresolved = true
	if !reflect.DeepEqual(*begun, want) {
		t.Fatalf("begun = %+v, want %+v", *begun, want)
	}
	if stored, err := repo.GetMediaPublication(ctx, input.Key); err != nil || !reflect.DeepEqual(*stored, want) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if err := repo.ConfirmMediaPublication(ctx, input.Key, "bbb"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteMediaPublication(ctx, input.Key); err != nil {
		t.Fatal(err)
	}
	if stored, err := repo.GetMediaPublication(ctx, input.Key); err != nil || !stored.Unresolved {
		t.Fatalf("wrong digest confirmed or unresolved upload deleted: %+v, %v", stored, err)
	}
	if err := repo.ConfirmMediaPublication(ctx, input.Key, input.Digest); err != nil {
		t.Fatal(err)
	}
	want.Unresolved = false
	if stored, err := repo.GetMediaPublication(ctx, input.Key); err != nil || !reflect.DeepEqual(*stored, want) {
		t.Fatalf("confirmed = %+v, %v", stored, err)
	}
	reopened, err := repo.BeginMediaPublication(ctx, input)
	if err != nil || !reopened.Unresolved || reopened.Digest != input.Digest {
		t.Fatalf("republishing identical content: %+v, %v", reopened, err)
	}
	if err := repo.ConfirmMediaPublication(ctx, input.Key, input.Digest); err != nil {
		t.Fatal(err)
	}
	for name, conflict := range map[string]repository.MediaPublication{
		"digest": {Key: input.Key, VideoID: input.VideoID, Digest: "ccc", SizeBytes: 10},
		"owner":  {Key: input.Key, VideoID: 2, Digest: input.Digest, SizeBytes: 10},
	} {
		if _, err := repo.BeginMediaPublication(ctx, conflict); err == nil {
			t.Fatalf("conflicting %s accepted", name)
		}
		if stored, err := repo.GetMediaPublication(ctx, input.Key); err != nil || !reflect.DeepEqual(*stored, want) {
			t.Fatalf("conflicting %s changed the row: %+v, %v", name, stored, err)
		}
	}
	if err := repo.RequestMediaPublicationDelete(ctx, input.Key); err != nil {
		t.Fatal(err)
	}
	want.DeleteRequested = true
	if stored, err := repo.GetMediaPublication(ctx, input.Key); err != nil || !reflect.DeepEqual(*stored, want) {
		t.Fatalf("delete request = %+v, %v", stored, err)
	}
	if _, err := repo.BeginMediaPublication(ctx, input); err == nil {
		t.Fatal("republished a key pending deletion")
	}
	if stored, err := repo.GetMediaPublication(ctx, input.Key); err != nil || !reflect.DeepEqual(*stored, want) {
		t.Fatalf("rejected republish changed the row: %+v, %v", stored, err)
	}
	if err := repo.DeleteMediaPublication(ctx, input.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetMediaPublication(ctx, input.Key); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("resolved publication survived deletion: %v", err)
	}
	pending := repository.MediaPublication{Key: "videos/1/waveform.json", VideoID: 1, Digest: "ddd", SizeBytes: 5}
	if _, err := repo.BeginMediaPublication(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := repo.RequestMediaPublicationDelete(ctx, pending.Key); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteMediaPublication(ctx, pending.Key); err != nil {
		t.Fatal(err)
	}
	if stored, err := repo.GetMediaPublication(ctx, pending.Key); err != nil || !stored.Unresolved || !stored.DeleteRequested {
		t.Fatalf("unresolved upload deleted before its outcome was known: %+v, %v", stored, err)
	}
	if err := repo.ConfirmMediaPublication(ctx, pending.Key, pending.Digest); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteMediaPublication(ctx, pending.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetMediaPublication(ctx, pending.Key); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("confirmed publication survived deletion: %v", err)
	}
}

func testMediaPublicationListing(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	for key, video := range map[string]int64{"a/1": 1, "a/2": 1, "a/3": 1, "a/10": 2, "b/1": 2, "b/2": 2} {
		if _, err := repo.BeginMediaPublication(ctx, repository.MediaPublication{Key: key, VideoID: video, Digest: key}); err != nil {
			t.Fatal(err)
		}
	}
	keysOf := func(rows []repository.MediaPublication) []string {
		out := make([]string, len(rows))
		for i, row := range rows {
			out[i] = row.Key
		}
		return out
	}
	var got []string
	for after := ""; ; {
		rows, err := repo.ListMediaPublications(ctx, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) > 2 {
			t.Fatalf("unbounded page: %d", len(rows))
		}
		if len(rows) == 0 {
			break
		}
		got = append(got, keysOf(rows)...)
		after = rows[len(rows)-1].Key
	}
	if want := []string{"a/1", "a/10", "a/2", "a/3", "b/1", "b/2"}; !slices.Equal(got, want) {
		t.Fatalf("all publications = %v, want %v", got, want)
	}
	if rows, err := repo.ListMediaPublications(ctx, "zzz", 10); err != nil || len(rows) != 0 {
		t.Fatalf("exhausted cursor = %v, %v", keysOf(rows), err)
	}
	rows, err := repo.ListRecordingPublications(ctx, 1, "", 2)
	if err != nil || !slices.Equal(keysOf(rows), []string{"a/1", "a/2"}) {
		t.Fatalf("first recording page = %v, %v", keysOf(rows), err)
	}
	rows, err = repo.ListRecordingPublications(ctx, 1, "a/2", 2)
	if err != nil || !slices.Equal(keysOf(rows), []string{"a/3"}) {
		t.Fatalf("second recording page = %v, %v", keysOf(rows), err)
	}
	rows, err = repo.ListRecordingPublications(ctx, 2, "", 10)
	if err != nil || !slices.Equal(keysOf(rows), []string{"a/10", "b/1", "b/2"}) {
		t.Fatalf("other recording = %v, %v", keysOf(rows), err)
	}
	if rows, err := repo.ListRecordingPublications(ctx, 3, "", 10); err != nil || len(rows) != 0 {
		t.Fatalf("recording without publications = %v, %v", keysOf(rows), err)
	}
}
