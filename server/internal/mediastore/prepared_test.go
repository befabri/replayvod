package mediastore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

type countedPreparedInput struct {
	reader *strings.Reader
	bytes  int
}

func (r *countedPreparedInput) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += n
	return n, err
}
func (r *countedPreparedInput) Seek(offset int64, whence int) (int64, error) {
	return r.reader.Seek(offset, whence)
}

func TestSavePreparedValidatesDuringPublicationHash(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			repo, raw, v := mediaFixture(t)
			owned, err := managed(t, repo, raw).Lock(t.Context(), v.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			body := "immutable prepared media"
			hash := sha256.Sum256([]byte(body))
			if changed {
				body = "tampered media"
			}
			input := &countedPreparedInput{reader: strings.NewReader(body)}
			err = owned.SavePrepared(t.Context(), "videos/prepared.mp4", input, hex.EncodeToString(hash[:]))
			if changed {
				if !errors.Is(err, ErrContentChanged) {
					t.Fatalf("changed input accepted: %v", err)
				}
				if _, err := repo.GetMediaPublication(t.Context(), "videos/prepared.mp4"); !errors.Is(err, repository.ErrNotFound) {
					t.Fatalf("changed bytes journaled: %v", err)
				}
				if exists, err := raw.Exists(t.Context(), "videos/prepared.mp4"); err != nil || exists {
					t.Fatalf("changed bytes uploaded: %v, %v", exists, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if input.bytes != 2*len(body) {
				t.Fatalf("input read %d bytes; want one validation pass and one upload (%d)", input.bytes, 2*len(body))
			}
		})
	}
}
