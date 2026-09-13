package mediatest

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/recordinglock"
)

func TestWithLocksFixturesLeaveRegistryAfterOwnerCleanup(t *testing.T) {
	var stores []*mediastore.Store
	t.Run("owner", func(t *testing.T) {
		original := New(t, nil, nil, nil, nil)
		clone := WithLocks(original, &recordinglock.Locks{})
		stores = []*mediastore.Store{original, clone, WithLocks(clone, &recordinglock.Locks{})}
		for i, store := range stores {
			if _, ok := fixtures.Load(store); !ok {
				t.Fatalf("fixture %d was not registered", i)
			}
		}
	})
	for i, store := range stores {
		if _, ok := fixtures.Load(store); ok {
			t.Errorf("fixture %d retained after its owning test cleaned up", i)
		}
	}
}
