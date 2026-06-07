package contracttest

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testNotFoundOnMissingGet pins that both adapters translate their driver's
// "no rows" error to the shared repository.ErrNotFound sentinel, across both a
// string-keyed (GetUser) and an int-keyed (GetVideo) lookup.
func testNotFoundOnMissingGet(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.GetUser(ctx, "nonexistent"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetUser missing: got %v, want ErrNotFound", err)
	}
	if _, err := repo.GetVideo(ctx, 999999); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetVideo missing: got %v, want ErrNotFound", err)
	}
}
