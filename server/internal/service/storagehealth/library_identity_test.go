package storagehealth

import (
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

func TestFirstAttachRequiresLibraryWitnessWithExistingMarker(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrelated storage", true: "restored matching storage"}[present], func(t *testing.T) {
			f := newFixture(t)
			f.seedFinishedRecording(t, "saved-recording", present)
			id := mustID(t)
			f.writeMarker(t, id)
			st, err := f.mon.Attach(f.ctx)
			if present {
				if err != nil || st.State != StateAttached || f.storedID(t) != id {
					t.Fatalf("matching marked storage did not attach: %+v %v", st, err)
				}
				return
			}
			if !errors.Is(err, storage.ErrUnattached) || st.Readable() || f.storedID(t) != "" {
				t.Fatalf("unrelated marker authenticated an existing library: %+v %v", st, err)
			}
			if got, err := storage.ReadMarker(f.ctx, f.store); err != nil || got != id {
				t.Fatalf("refused attach changed foreign marker: %q %v", got, err)
			}
			// An explicit owner choice still permits starting over on this disk.
			if _, err := f.repo.UpsertUser(f.ctx, &repository.User{ID: "owner", Login: "owner", DisplayName: "Owner", Role: "owner"}); err != nil {
				t.Fatal(err)
			}
			if st, err := f.mon.Adopt(f.ctx, "owner"); err != nil || st.State != StateAttached || f.storedID(t) != id {
				t.Fatalf("explicit adoption did not record selected identity: %+v %v", st, err)
			}
		})
	}
}
